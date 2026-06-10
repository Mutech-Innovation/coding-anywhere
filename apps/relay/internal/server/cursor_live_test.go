//go:build cursorlive

package server_test

// Full-stack live test: raw WS client → in-process relay → daemon
// (relayclient + chatrouter with the DEFAULT builders) → real
// `cursor-agent acp` subprocess.
//
// Run: go test -tags cursorlive ./apps/relay/internal/server/ -run TestLiveCursorFullStack -v
// Requires: cursor-agent on PATH + `cursor-agent login` done.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/chatrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestLiveCursorFullStack(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Pair daemon + client.
	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	// Daemon side with DEFAULT builders → real `cursor-agent acp`.
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	cr := chatrouter.New(dcli, logger, nil)
	t.Cleanup(func() { _ = cr.Close() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	envCh := make(chan wire.Envelope, 256)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, clientWS, &env); err != nil {
				return
			}
			envCh <- env
		}
	}()
	awaitResp := func(id string, timeout time.Duration) wire.Envelope {
		t.Helper()
		deadline := time.After(timeout)
		for {
			select {
			case env := <-envCh:
				if env.ID == id && env.Type == wire.TypeResponse {
					return env
				}
			case <-deadline:
				t.Fatalf("timeout waiting for response %s", id)
			}
		}
	}

	// 1. chat.agents must report cursor-cli available (binary on PATH).
	req, _ := wire.MakeRequest("a-1", wire.ChannelChat, wire.ChatMethodAgents, struct{}{})
	_ = wsjson.Write(ctx, clientWS, req)
	res := awaitResp("a-1", 5*time.Second)
	var agents wire.ChatAgentsResponse
	_ = json.Unmarshal(res.Payload, &agents)
	cursorAvailable := false
	for _, a := range agents.Agents {
		t.Logf("agent %s available=%v reason=%q", a.ID, a.Available, a.Reason)
		if a.ID == "cursor-cli" && a.Available {
			cursorAvailable = true
		}
	}
	if !cursorAvailable {
		t.Fatal("cursor-cli not reported available")
	}

	// 2. create_session against the real cursor-agent.
	cwd, _ := os.Getwd()
	req, _ = wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "cursor-cli", Cwd: cwd})
	_ = wsjson.Write(ctx, clientWS, req)
	res = awaitResp("c-1", 60*time.Second)
	if res.Error != nil {
		t.Fatalf("create_session: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	_ = json.Unmarshal(res.Payload, &createRes)
	t.Logf("session=%s native=%s modes=%d current=%s pending=%d",
		createRes.Session.ID, createRes.Session.NativeID,
		len(createRes.AvailableModes), createRes.CurrentModeID,
		len(createRes.PendingUpdates))

	// Model config option must have been synthesised from the models payload.
	hasModelOption := false
	for _, u := range createRes.PendingUpdates {
		if strings.Contains(string(u), "config_option_update") &&
			strings.Contains(string(u), `"id":"model"`) {
			hasModelOption = true
		}
	}
	if !hasModelOption {
		t.Fatal("no synthetic model config option in pending updates")
	}
	t.Log("synthetic model config option OK")

	// 3. prompt → expect streamed chunks then end_turn.
	req, _ = wire.MakeRequest("p-1", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: createRes.Session.ID, Text: "Reply with exactly: PONG"})
	_ = wsjson.Write(ctx, clientWS, req)

	gotChunk := false
	deadline := time.After(120 * time.Second)
	for {
		var env wire.Envelope
		select {
		case env = <-envCh:
		case <-deadline:
			t.Fatal("timeout waiting for prompt flow")
		}
		if env.Type == wire.TypeEvent && env.Method == wire.ChatEventUpdate &&
			strings.Contains(string(env.Payload), "agent_message_chunk") {
			gotChunk = true
		}
		if env.Type == wire.TypeResponse && env.ID == "p-1" {
			if env.Error != nil {
				t.Fatalf("prompt: %+v", env.Error)
			}
			var pr wire.ChatPromptResponse
			_ = json.Unmarshal(env.Payload, &pr)
			t.Logf("stopReason=%s gotChunk=%v", pr.StopReason, gotChunk)
			if pr.StopReason != "end_turn" || !gotChunk {
				t.Fatalf("unexpected end state: stop=%s chunk=%v", pr.StopReason, gotChunk)
			}
			return
		}
	}
}
