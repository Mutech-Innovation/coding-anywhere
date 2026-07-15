package relayclient_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// mockRelay is a tiny stand-in for the real relay used by relayclient tests.
// It captures the Authorization header on connect, then echoes meta.ping
// envelopes back as meta.pong responses. If `closeOnPing` is true, the mock
// closes the WebSocket after replying to the first ping — used to drive the
// reconnect test.
type mockRelay struct {
	t *testing.T

	closeOnPing atomic.Bool

	mu       sync.Mutex
	gotAuth  string
	connects atomic.Int64
	pings    atomic.Int64
}

func (m *mockRelay) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ws/connect", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.gotAuth = r.Header.Get("Authorization")
		m.mu.Unlock()
		m.connects.Add(1)

		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			m.t.Logf("Accept: %v", err)
			return
		}
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		ctx := r.Context()
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, ws, &env); err != nil {
				return
			}
			if env.Channel == wire.ChannelMeta && env.Method == "ping" {
				m.pings.Add(1)
				resp, _ := wire.MakeResponse(env.ID, wire.ChannelMeta,
					map[string]any{"nonce": "ok", "server_time_ms": time.Now().UnixMilli()},
					nil)
				if err := wsjson.Write(ctx, ws, resp); err != nil {
					return
				}
				if m.closeOnPing.Load() {
					_ = ws.Close(websocket.StatusGoingAway, "going down")
					return
				}
			}
		}
	})
	return mux
}

func TestRelayClientConnectsAndPings(t *testing.T) {
	mock := &mockRelay{t: t}
	hs := httptest.NewServer(mock.handler())
	t.Cleanup(hs.Close)

	cli := relayclient.New(relayclient.Config{
		RelayURL:         strings.Replace(hs.URL, "http", "ws", 1),
		AccessToken:      "test-token",
		PingInterval:     150 * time.Millisecond,
		HandshakeTimeout: 3 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	gotPong := make(chan struct{}, 1)
	cli.Handle(wire.ChannelMeta, func(env wire.Envelope) error {
		select {
		case gotPong <- struct{}{}:
		default:
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientErr := make(chan error, 1)
	go func() { clientErr <- cli.Run(ctx) }()

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer connectCancel()
	if err := cli.WaitConnected(connectCtx); err != nil {
		t.Fatalf("WaitConnected: %v", err)
	}

	select {
	case <-gotPong:
	case <-time.After(2 * time.Second):
		t.Fatal("no pong within 2s")
	}

	mock.mu.Lock()
	auth := mock.gotAuth
	mock.mu.Unlock()
	if auth != "Bearer test-token" {
		t.Fatalf("Authorization header: %q", auth)
	}
	if mock.pings.Load() == 0 {
		t.Fatal("server saw no pings")
	}

	cancel()
	if err := <-clientErr; err != nil && err != context.Canceled {
		t.Fatalf("Run returned %v", err)
	}
}

func TestRelayClientSendsCustomEnvelopes(t *testing.T) {
	mock := &mockRelay{t: t}
	hs := httptest.NewServer(mock.handler())
	t.Cleanup(hs.Close)

	cli := relayclient.New(relayclient.Config{
		RelayURL:         strings.Replace(hs.URL, "http", "ws", 1),
		AccessToken:      "tok",
		PingInterval:     time.Hour, // disable
		HandshakeTimeout: 3 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientErr := make(chan error, 1)
	go func() { clientErr <- cli.Run(ctx) }()
	if err := cli.WaitConnected(context.Background()); err != nil {
		t.Fatalf("WaitConnected: %v", err)
	}

	req, err := wire.MakeRequest("ping-id", wire.ChannelMeta, "ping", map[string]string{"nonce": "x"})
	if err != nil {
		t.Fatal(err)
	}

	gotResp := make(chan wire.Envelope, 1)
	cli.Handle(wire.ChannelMeta, func(env wire.Envelope) error {
		gotResp <- env
		return nil
	})

	if !cli.Send(req) {
		t.Fatal("Send returned false")
	}

	select {
	case env := <-gotResp:
		if env.ID != "ping-id" {
			t.Fatalf("envelope id mismatch: %+v", env)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no response within 2s")
	}

	cancel()
	<-clientErr
}

// TestRelayClientBackoffResetsAfterHealthySession proves that sessions that
// outlive BackoffResetAfter reset the reconnect delay: the mock kills every
// session right after the first ping (~50ms in), and with BackoffResetAfter
// below that the client must keep re-dialing at the base ~250ms delay. Under
// the old always-growing backoff, reaching 8 connects would take >30s
// (0.25+0.5+1+2+4+8+16+...), far past this test's 4s deadline.
func TestRelayClientBackoffResetsAfterHealthySession(t *testing.T) {
	mock := &mockRelay{t: t}
	mock.closeOnPing.Store(true)
	hs := httptest.NewServer(mock.handler())
	t.Cleanup(hs.Close)

	cli := relayclient.New(relayclient.Config{
		RelayURL:          strings.Replace(hs.URL, "http", "ws", 1),
		AccessToken:       "tok",
		PingInterval:      50 * time.Millisecond,
		HandshakeTimeout:  1 * time.Second,
		BackoffResetAfter: time.Millisecond,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cli.Handle(wire.ChannelMeta, func(wire.Envelope) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = cli.Run(ctx) }()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if mock.connects.Load() >= 8 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected >= 8 connects within 4s (backoff should have reset), got %d", mock.connects.Load())
}

func TestRelayClientRejectsOversizedEnvelope(t *testing.T) {
	cli := relayclient.New(relayclient.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	env, err := wire.MakeEvent("oversized", wire.ChannelChat, "update", map[string]string{
		"data": strings.Repeat("x", wire.MaxEnvelopeBytes),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cli.Send(env) {
		t.Fatal("oversized envelope should be rejected before it reaches the relay")
	}
}

func TestRelayClientReconnects(t *testing.T) {
	mock := &mockRelay{t: t}
	mock.closeOnPing.Store(true)
	hs := httptest.NewServer(mock.handler())
	t.Cleanup(hs.Close)

	cli := relayclient.New(relayclient.Config{
		RelayURL:         strings.Replace(hs.URL, "http", "ws", 1),
		AccessToken:      "tok",
		PingInterval:     150 * time.Millisecond,
		HandshakeTimeout: 1 * time.Second,
		MaxBackoff:       150 * time.Millisecond,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cli.Handle(wire.ChannelMeta, func(wire.Envelope) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = cli.Run(ctx) }()

	// Wait for at least 2 connect attempts — proves reconnect kicked in
	// after the mock disconnected on first ping.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mock.connects.Load() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := mock.connects.Load(); got < 2 {
		t.Fatalf("expected >= 2 connects (reconnect), got %d", got)
	}
}
