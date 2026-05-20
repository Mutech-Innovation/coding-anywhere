package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/server"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func newTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := server.Defaults()
	cfg.JWTSecret = []byte("0123456789abcdef0123456789abcdef")
	cfg.ListenAddr = ":0"
	srv, err := server.New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	// We don't use srv.Run because httptest.Server gives us a random port and
	// dispatches via the same handler chain.
	hs := httptest.NewServer(srvHandler(t, srv))
	t.Cleanup(hs.Close)
	return hs, srv
}

// srvHandler reaches into Server's routes via a tiny shim that mirrors what
// (*Server).Run wires up. We expose a test-only handler builder rather than
// exporting `routes()` from the production package.
func srvHandler(t *testing.T, srv *server.Server) http.Handler {
	t.Helper()
	return server.HandlerForTest(srv)
}

func TestHealthz(t *testing.T) {
	hs, _ := newTestServer(t)

	res, err := http.Get(hs.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer closeBody(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("body: %+v", body)
	}
}

func TestWSConnectRequiresAuth(t *testing.T) {
	hs, _ := newTestServer(t)

	wsURL := strings.Replace(hs.URL, "http", "ws", 1) + "/v1/ws/connect"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, res, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatalf("expected auth error, got accepted connection")
	}
	if res == nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got status=%v err=%v", res, err)
	}
}

func TestWSPingPong(t *testing.T) {
	hs, srv := newTestServer(t)

	tok, err := srv.Signer().Issue("acc-1", "dev-1", auth.DeviceClient, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	wsURL := strings.Replace(hs.URL, "http", "ws", 1) + "/v1/ws/connect?token=" + tok
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	req, err := wire.MakeRequest("req-1", wire.ChannelMeta, "ping", map[string]string{"nonce": "n1"})
	if err != nil {
		t.Fatalf("MakeRequest: %v", err)
	}
	if err := wsjson.Write(ctx, conn, req); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var res wire.Envelope
	if err := wsjson.Read(ctx, conn, &res); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.ID != "req-1" || res.Type != wire.TypeResponse || res.Channel != wire.ChannelMeta {
		t.Fatalf("unexpected envelope: %+v", res)
	}
	var body struct {
		Nonce        string `json:"nonce"`
		ServerTimeMS int64  `json:"server_time_ms"`
	}
	if err := json.Unmarshal(res.Payload, &body); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if body.Nonce != "n1" {
		t.Fatalf("nonce: %q", body.Nonce)
	}
	if body.ServerTimeMS == 0 {
		t.Fatal("server_time_ms unset")
	}
}

func TestWSUnknownChannelReturnsError(t *testing.T) {
	hs, srv := newTestServer(t)
	tok, err := srv.Signer().Issue("acc-1", "dev-1", auth.DeviceClient, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	wsURL := strings.Replace(hs.URL, "http", "ws", 1) + "/v1/ws/connect?token=" + tok
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Bypass MakeRequest's channel validation by hand-writing the envelope.
	bad := wire.Envelope{ID: "req-2", Type: wire.TypeRequest, Channel: "mystery", Method: "x"}
	if err := wsjson.Write(ctx, conn, bad); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var res wire.Envelope
	if err := wsjson.Read(ctx, conn, &res); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.Error == nil || res.Error.Code != "unknown_channel" {
		t.Fatalf("expected unknown_channel error, got %+v", res)
	}
}
