package server_test

import (
	"context"
	"encoding/json"
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

func TestHubBroadcastAcceptsMultiMiBEnvelope(t *testing.T) {
	hs, srv := newTestServer(t)
	daemon := dialWS(t, hs.URL, srv.Signer(), "acc-1", "dev-daemon", auth.DeviceDaemon)
	client := dialWS(t, hs.URL, srv.Signer(), "acc-1", "dev-client", auth.DeviceClient)
	client.SetReadLimit(wire.MaxEnvelopeBytes)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body := strings.Repeat("x", 2<<20)
	ev, err := wire.MakeEvent("evt-large", wire.ChannelFiles, "preview", map[string]string{"data": body})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, daemon, ev); err != nil {
		t.Fatalf("daemon Write: %v", err)
	}
	var got wire.Envelope
	if err := wsjson.Read(ctx, client, &got); err != nil {
		t.Fatalf("client Read: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["data"] != body {
		t.Fatalf("large payload was corrupted: got %d bytes", len(payload["data"]))
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

func TestHubClientRequestNoDaemonGetsError(t *testing.T) {
	hs, srv := newTestServer(t)
	client := dialWS(t, hs.URL, srv.Signer(), "acc-1", "client", auth.DeviceClient)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := wire.MakeRequest("req-no-daemon", wire.ChannelFiles, wire.FilesMethodListWorkspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, client, req); err != nil {
		t.Fatalf("client Write: %v", err)
	}
	var got wire.Envelope
	if err := wsjson.Read(ctx, client, &got); err != nil {
		t.Fatalf("client Read: %v", err)
	}
	if got.Type != wire.TypeResponse || got.ID != req.ID {
		t.Fatalf("unexpected envelope: %+v", got)
	}
	if got.Error == nil || got.Error.Code != "no_daemon" {
		t.Fatalf("expected no_daemon error, got %+v", got.Error)
	}
}

func TestHubClientStaleCloseKeepsLiveConn(t *testing.T) {
	hs, srv := newTestServer(t)
	daemon := dialWS(t, hs.URL, srv.Signer(), "acc-1", "daemon", auth.DeviceDaemon)
	// Same device_id as a browser tab reconnecting after a drop.
	client1 := dialWS(t, hs.URL, srv.Signer(), "acc-1", "browser", auth.DeviceClient)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := wire.MakeRequest("req-1", wire.ChannelSessions, "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, client1, req); err != nil {
		t.Fatalf("client1 Write: %v", err)
	}
	var got wire.Envelope
	if err := wsjson.Read(ctx, daemon, &got); err != nil {
		t.Fatalf("daemon Read after client1: %v", err)
	}
	if got.ID != "req-1" {
		t.Fatalf("unexpected envelope: %+v", got)
	}

	// Simulate auto-reconnect: second socket for the same device_id.
	client2, _, err := websocket.Dial(ctx, strings.Replace(hs.URL, "http", "ws", 1)+"/v1/ws/connect?token="+mustToken(t, srv.Signer(), "acc-1", "browser", auth.DeviceClient), nil)
	if err != nil {
		t.Fatalf("client2 Dial: %v", err)
	}
	t.Cleanup(func() { _ = client2.Close(websocket.StatusNormalClosure, "") })
	time.Sleep(50 * time.Millisecond)

	// Close the superseded socket; hub must keep routing to client2.
	if err := client1.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("client1 Close: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	req2, err := wire.MakeRequest("req-2", wire.ChannelSessions, "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, client2, req2); err != nil {
		t.Fatalf("client2 Write: %v", err)
	}
	var got2 wire.Envelope
	if err := wsjson.Read(ctx, daemon, &got2); err != nil {
		t.Fatalf("daemon Read after client2: %v", err)
	}
	if got2.ID != "req-2" {
		t.Fatalf("unexpected envelope: %+v", got2)
	}
}

func mustToken(t *testing.T, signer interface {
	Issue(string, string, auth.DeviceKind, time.Duration) (string, error)
}, account, device string, kind auth.DeviceKind) string {
	t.Helper()
	tok, err := signer.Issue(account, device, kind, time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return tok
}

// TestHubSameDeviceClientTabsCoexist covers the multi-tab case: two live
// client connections sharing one device_id (same browser localStorage) must
// both stay registered — neither evicts the other — and both receive daemon
// fan-out and can reach the daemon.
func TestHubSameDeviceClientTabsCoexist(t *testing.T) {
	hs, srv := newTestServer(t)
	daemon := dialWS(t, hs.URL, srv.Signer(), "acc-1", "daemon", auth.DeviceDaemon)
	tab1 := dialWS(t, hs.URL, srv.Signer(), "acc-1", "browser", auth.DeviceClient)
	tab2 := dialWS(t, hs.URL, srv.Signer(), "acc-1", "browser", auth.DeviceClient)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Daemon event must reach BOTH tabs. Under the old device_id keying,
	// tab2's Register would have closed tab1 with reason "replaced".
	ev, err := wire.MakeEvent("evt-tabs", wire.ChannelChat, "agent_message", map[string]string{"text": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, daemon, ev); err != nil {
		t.Fatal(err)
	}
	for i, tab := range []*websocket.Conn{tab1, tab2} {
		var got wire.Envelope
		if err := wsjson.Read(ctx, tab, &got); err != nil {
			t.Fatalf("tab%d Read: %v", i+1, err)
		}
		if got.ID != "evt-tabs" {
			t.Fatalf("tab%d unexpected envelope: %+v", i+1, got)
		}
	}

	// And both tabs can still reach the daemon.
	for i, tab := range []*websocket.Conn{tab1, tab2} {
		id := []string{"req-tab1", "req-tab2"}[i]
		req, err := wire.MakeRequest(id, wire.ChannelSessions, "list", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Write(ctx, tab, req); err != nil {
			t.Fatalf("tab%d Write: %v", i+1, err)
		}
		var got wire.Envelope
		if err := wsjson.Read(ctx, daemon, &got); err != nil {
			t.Fatalf("daemon Read for tab%d: %v", i+1, err)
		}
		if got.ID != id {
			t.Fatalf("daemon got %+v, want id %s", got, id)
		}
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
