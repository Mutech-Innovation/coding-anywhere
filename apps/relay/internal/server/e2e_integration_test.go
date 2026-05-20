package server_test

// End-to-end integration tests that exercise the full pair → daemon
// (relayclient) → relay (server) → client (raw WebSocket) loop. They assert
// that an event emitted by a paired daemon reaches a paired client through
// the relay, and vice versa.

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestEndToEndPairAndEcho(t *testing.T) {
	hs, _ := newPairingTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pc := pairingclient.New(hs.URL)

	// 1. Daemon initiates pairing.
	init, err := pc.Init(ctx, "daemon", "test-laptop", "")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// 2. Client (the simulated web/mobile app) claims the code.
	claim, err := pc.Claim(ctx, init.Code, "client", "test-browser", "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	clientCred := claim.Credentials

	// 3. Daemon polls status to retrieve its own credentials.
	status, err := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitClaimed: %v", err)
	}
	daemonCred := status

	if clientCred.AccountID != daemonCred.AccountID {
		t.Fatalf("accounts differ: %s vs %s", clientCred.AccountID, daemonCred.AccountID)
	}

	// 4. Daemon spins up the relay client.
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dc := relayclient.New(relayclient.Config{
		RelayURL:         relayURL,
		AccessToken:      daemonCred.AccessToken,
		PingInterval:     time.Second,
		HandshakeTimeout: 3 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var sessionsSeenOnDaemon atomic.Int64
	gotOnDaemon := make(chan wire.Envelope, 4)
	dc.Handle(wire.ChannelSessions, func(env wire.Envelope) error {
		sessionsSeenOnDaemon.Add(1)
		select {
		case gotOnDaemon <- env:
		default:
		}
		return nil
	})
	dc.Handle(wire.ChannelMeta, func(wire.Envelope) error { return nil })

	clientErr := make(chan error, 1)
	go func() { clientErr <- dc.Run(ctx) }()
	if err := dc.WaitConnected(ctx); err != nil {
		t.Fatalf("daemon WaitConnected: %v", err)
	}

	// 5. Client opens a raw WS using its access token.
	clientWS, _, err := websocket.Dial(
		ctx,
		relayURL+"/v1/ws/connect?token="+clientCred.AccessToken,
		nil,
	)
	if err != nil {
		t.Fatalf("client Dial: %v", err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	// Give both sides a moment to register.
	time.Sleep(100 * time.Millisecond)

	// 6. Daemon emits a sessions event; client must receive it.
	ev, err := wire.MakeEvent("evt-1", wire.ChannelSessions, "list",
		map[string]any{"sessions": []string{"s-1", "s-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if !dc.Send(ev) {
		t.Fatal("daemon Send returned false")
	}

	var clientGot wire.Envelope
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	if err := wsjson.Read(readCtx, clientWS, &clientGot); err != nil {
		readCancel()
		t.Fatalf("client Read: %v", err)
	}
	readCancel()
	if clientGot.ID != "evt-1" || clientGot.Channel != wire.ChannelSessions || clientGot.Method != "list" {
		t.Fatalf("client envelope: %+v", clientGot)
	}

	// 7. Client emits a sessions request; daemon must receive it.
	req, err := wire.MakeRequest("req-1", wire.ChannelSessions, "create",
		map[string]any{"workspace": "/Users/me/app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, clientWS, req); err != nil {
		t.Fatalf("client Write: %v", err)
	}

	select {
	case got := <-gotOnDaemon:
		if got.ID != "req-1" || got.Channel != wire.ChannelSessions || got.Method != "create" {
			t.Fatalf("daemon envelope: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not receive client envelope within 2s")
	}

	cancel()
	if err := <-clientErr; err != nil && err != context.Canceled {
		t.Fatalf("daemon Run returned %v", err)
	}
}
