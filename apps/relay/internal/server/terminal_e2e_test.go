package server_test

// End-to-end integration test for the `terminal` channel.
//
// The flow:
//   1. Spin up a real relay (server + sqlite store).
//   2. Pair daemon + client via the pair endpoints.
//   3. Stand up a daemon-side relayclient bound to a terminalrouter.
//   4. Connect a raw WebSocket as the client device.
//   5. Send terminal.open → expect a session id back.
//   6. Send terminal.input "echo PTY-PROOF\n" (base64 encoded).
//   7. Wait for terminal.data events whose decoded chunks contain
//      "PTY-PROOF" — proves the round-trip works through the relay.
//   8. Send terminal.close → expect ack.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/terminalrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestTerminalEndToEnd(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Pair.
	pc := pairingclient.New(hs.URL)
	init, err := pc.Init(ctx, "daemon", "test-laptop", "")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	claim, err := pc.Claim(ctx, init.Code, "client", "test-browser", "")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	clientCred := claim.Credentials
	daemonCred, err := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitClaimed: %v", err)
	}

	// 2. Daemon side: relay client + terminal router.
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dc := relayclient.New(relayclient.Config{
		RelayURL:         relayURL,
		AccessToken:      daemonCred.AccessToken,
		PingInterval:     time.Second,
		HandshakeTimeout: 3 * time.Second,
	}, logger)
	tr := terminalrouter.New(dc, logger)
	t.Cleanup(func() { _ = tr.CloseAll() })

	go func() { _ = dc.Run(ctx) }()
	if err := dc.WaitConnected(ctx); err != nil {
		t.Fatalf("daemon WaitConnected: %v", err)
	}

	// 3. Client side: raw WS.
	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+clientCred.AccessToken, nil)
	if err != nil {
		t.Fatalf("client Dial: %v", err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	// Reader: collect every envelope the client receives, indexed by id.
	type recv struct {
		env wire.Envelope
		err error
	}
	envCh := make(chan recv, 64)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
				select {
				case envCh <- recv{err: err}:
				case <-ctx.Done():
				}
				return
			}
			envCh <- recv{env: env}
		}
	}()

	awaitResp := func(id string) wire.Envelope {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case r := <-envCh:
				if r.err != nil {
					t.Fatalf("recv err: %v", r.err)
				}
				if r.env.ID == id && r.env.Type == wire.TypeResponse {
					return r.env
				}
			case <-deadline:
				t.Fatal("timeout waiting for response", id)
			}
		}
	}

	awaitDataContaining := func(sessionID, want string) {
		t.Helper()
		var collected strings.Builder
		deadline := time.After(5 * time.Second)
		for {
			select {
			case r := <-envCh:
				if r.err != nil {
					t.Fatalf("recv err: %v", r.err)
				}
				if r.env.Channel != wire.ChannelTerminal || r.env.Method != wire.TerminalEventData {
					continue
				}
				var ev wire.TerminalDataEvent
				if err := json.Unmarshal(r.env.Payload, &ev); err != nil {
					t.Fatalf("data event: %v", err)
				}
				if ev.ID != sessionID {
					continue
				}
				bytes, err := base64.StdEncoding.DecodeString(ev.Data)
				if err != nil {
					t.Fatalf("base64: %v", err)
				}
				collected.Write(bytes)
				if strings.Contains(collected.String(), want) {
					return
				}
			case <-deadline:
				t.Fatalf("timeout waiting for %q in terminal.data; got %q", want, collected.String())
			}
		}
	}

	// 4. Open a terminal.
	openReq, err := wire.MakeRequest("open-1", wire.ChannelTerminal, wire.TerminalMethodOpen,
		wire.TerminalOpenRequest{Command: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, clientWS, openReq); err != nil {
		t.Fatalf("open Write: %v", err)
	}
	res := awaitResp("open-1")
	if res.Error != nil {
		t.Fatalf("open returned error: %+v", res.Error)
	}
	var openRes wire.TerminalOpenResponse
	if err := json.Unmarshal(res.Payload, &openRes); err != nil {
		t.Fatalf("open payload: %v", err)
	}
	sessionID := openRes.Session.ID
	if sessionID == "" || openRes.Session.PID == 0 {
		t.Fatalf("open response missing fields: %+v", openRes)
	}

	// 5. Send input that produces a deterministic marker in the PTY.
	inputReq, err := wire.MakeRequest("input-1", wire.ChannelTerminal, wire.TerminalMethodInput,
		wire.TerminalInputRequest{
			ID:   sessionID,
			Data: base64.StdEncoding.EncodeToString([]byte("printf PTY-PROOF-OK\n")),
		})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, clientWS, inputReq); err != nil {
		t.Fatalf("input Write: %v", err)
	}
	awaitResp("input-1")
	awaitDataContaining(sessionID, "PTY-PROOF-OK")

	// 6. Resize.
	resizeReq, err := wire.MakeRequest("resize-1", wire.ChannelTerminal, wire.TerminalMethodResize,
		wire.TerminalResizeRequest{ID: sessionID, Cols: 132, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, clientWS, resizeReq); err != nil {
		t.Fatalf("resize Write: %v", err)
	}
	resResize := awaitResp("resize-1")
	if resResize.Error != nil {
		t.Fatalf("resize error: %+v", resResize.Error)
	}

	// 7. Close.
	closeReq, err := wire.MakeRequest("close-1", wire.ChannelTerminal, wire.TerminalMethodClose,
		wire.TerminalCloseRequest{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, clientWS, closeReq); err != nil {
		t.Fatalf("close Write: %v", err)
	}
	awaitResp("close-1")
}

func TestTerminalListReturnsRunningSessions(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	tr := terminalrouter.New(dcli, logger)
	t.Cleanup(func() { _ = tr.CloseAll() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	// Open one session, then list.
	openReq, _ := wire.MakeRequest("open-1", wire.ChannelTerminal, wire.TerminalMethodOpen,
		wire.TerminalOpenRequest{Command: []string{"/bin/sh"}})
	if err := wsjson.Write(ctx, clientWS, openReq); err != nil {
		t.Fatal(err)
	}
	listReq, _ := wire.MakeRequest("list-1", wire.ChannelTerminal, wire.TerminalMethodList, struct{}{})
	if err := wsjson.Write(ctx, clientWS, listReq); err != nil {
		t.Fatal(err)
	}

	// Read until we see the list response.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("no list response within 5s")
		default:
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
				t.Fatalf("read: %v", err)
			}
			if env.ID != "list-1" || env.Type != wire.TypeResponse {
				continue
			}
			var lr wire.TerminalListResponse
			if err := json.Unmarshal(env.Payload, &lr); err != nil {
				t.Fatalf("list payload: %v", err)
			}
			if len(lr.Sessions) == 0 {
				t.Fatalf("expected at least 1 session, got 0")
			}
			return
		}
	}
}
