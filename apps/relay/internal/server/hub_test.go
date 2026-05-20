package server_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// dialClient is a small helper that issues a JWT for `kind` and opens an
// authenticated WebSocket against the test server.
func dialWS(t *testing.T, hsURL string, signer interface {
	Issue(string, string, auth.DeviceKind, time.Duration) (string, error)
}, account, device string, kind auth.DeviceKind) *websocket.Conn {
	t.Helper()
	tok, err := signer.Issue(account, device, kind, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	wsURL := strings.Replace(hsURL, "http", "ws", 1) + "/v1/ws/connect?token=" + tok
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial(%s): %v", kind, err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func TestHubBroadcastDaemonToClient(t *testing.T) {
	hs, srv := newTestServer(t)

	daemon := dialWS(t, hs.URL, srv.Signer(), "acc-1", "dev-daemon", auth.DeviceDaemon)
	client := dialWS(t, hs.URL, srv.Signer(), "acc-1", "dev-client", auth.DeviceClient)
	// Tiny pause so both Register() calls have completed before the broadcast.
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Daemon emits a session-channel event.
	ev, err := wire.MakeEvent("evt-1", wire.ChannelSessions, "created", map[string]string{"id": "s-123"})
	if err != nil {
		t.Fatalf("MakeEvent: %v", err)
	}
	if err := wsjson.Write(ctx, daemon, ev); err != nil {
		t.Fatalf("daemon Write: %v", err)
	}

	// Client should receive it.
	var got wire.Envelope
	if err := wsjson.Read(ctx, client, &got); err != nil {
		t.Fatalf("client Read: %v", err)
	}
	if got.ID != "evt-1" || got.Channel != wire.ChannelSessions || got.Method != "created" {
		t.Fatalf("unexpected envelope: %+v", got)
	}

	// And critically: daemon must NOT receive its own message.
	deadlineCtx, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	var echo wire.Envelope
	if err := wsjson.Read(deadlineCtx, daemon, &echo); err == nil {
		t.Fatalf("daemon received echo: %+v", echo)
	}
}

func TestHubBroadcastClientToDaemon(t *testing.T) {
	hs, srv := newTestServer(t)
	daemon := dialWS(t, hs.URL, srv.Signer(), "acc-1", "dev-daemon", auth.DeviceDaemon)
	client := dialWS(t, hs.URL, srv.Signer(), "acc-1", "dev-client", auth.DeviceClient)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := wire.MakeRequest("req-1", wire.ChannelSessions, "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, client, req); err != nil {
		t.Fatalf("client Write: %v", err)
	}

	var got wire.Envelope
	if err := wsjson.Read(ctx, daemon, &got); err != nil {
		t.Fatalf("daemon Read: %v", err)
	}
	if got.ID != "req-1" || got.Method != "list" {
		t.Fatalf("unexpected envelope: %+v", got)
	}
}

func TestHubAccountIsolation(t *testing.T) {
	hs, srv := newTestServer(t)
	clientA := dialWS(t, hs.URL, srv.Signer(), "acc-A", "client-A", auth.DeviceClient)
	daemonB := dialWS(t, hs.URL, srv.Signer(), "acc-B", "daemon-B", auth.DeviceDaemon)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Client A speaks but has no daemon on its account, so daemon B (other
	// account) must not receive anything.
	req, err := wire.MakeRequest("req-x", wire.ChannelSessions, "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, clientA, req); err != nil {
		t.Fatalf("clientA Write: %v", err)
	}

	deadlineCtx, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	var got wire.Envelope
	if err := wsjson.Read(deadlineCtx, daemonB, &got); err == nil {
		t.Fatalf("daemonB unexpectedly received: %+v", got)
	}
}

func TestHubMultipleClientsFanOut(t *testing.T) {
	hs, srv := newTestServer(t)
	daemon := dialWS(t, hs.URL, srv.Signer(), "acc-1", "daemon", auth.DeviceDaemon)
	c1 := dialWS(t, hs.URL, srv.Signer(), "acc-1", "client-1", auth.DeviceClient)
	c2 := dialWS(t, hs.URL, srv.Signer(), "acc-1", "client-2", auth.DeviceClient)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ev, err := wire.MakeEvent("evt-1", wire.ChannelChat, "agent_message", map[string]string{"text": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, daemon, ev); err != nil {
		t.Fatal(err)
	}

	for _, c := range []*websocket.Conn{c1, c2} {
		var got wire.Envelope
		if err := wsjson.Read(ctx, c, &got); err != nil {
			t.Fatalf("client Read: %v", err)
		}
		if got.ID != "evt-1" {
			t.Fatalf("unexpected envelope: %+v", got)
		}
	}
}
