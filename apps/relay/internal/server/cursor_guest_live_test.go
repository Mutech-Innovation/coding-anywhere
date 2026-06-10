//go:build cursorlive

package server_test

// Live full-stack test for a guest "ask" share locked to a cursor-agent
// model:
//
//	owner WS ──(open cursor session, read model list)──▶ relay ─▶ daemon ─▶ cursor-agent
//	POST /v1/share/ask {agent_id: cursor-cli, cwd, model}
//	guest WS ──(create_session, no fields)──▶ relay injects ─▶ daemon force-ask + set_model
//
// Run: go test -tags cursorlive ./apps/relay/internal/server/ -run TestLiveCursorGuestModelLock -v
// Requires: cursor-agent on PATH + `cursor-agent login`.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/chatrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// warnRecorder captures slog warnings so the test can assert the daemon did
// NOT swallow a set_model failure.
type warnRecorder struct {
	slog.Handler
	mu    sync.Mutex
	warns []string
}

func (w *warnRecorder) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		w.mu.Lock()
		w.warns = append(w.warns, r.Message)
		w.mu.Unlock()
	}
	return w.Handler.Handle(ctx, r)
}

func TestLiveCursorGuestModelLock(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	rec := &warnRecorder{Handler: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})}
	logger := slog.New(rec)

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	cr := chatrouter.New(dcli, logger, nil) // DefaultBuilders → real cursor-agent
	t.Cleanup(func() { _ = cr.Close() })
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	dial := func(token string) (*websocket.Conn, chan wire.Envelope) {
		ws, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+token, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "") })
		ch := make(chan wire.Envelope, 256)
		go func() {
			for {
				var env wire.Envelope
				if err := wsjson.Read(ctx, ws, &env); err != nil {
					return
				}
				ch <- env
			}
		}()
		return ws, ch
	}
	awaitResp := func(ch chan wire.Envelope, id string, timeout time.Duration) wire.Envelope {
		t.Helper()
		deadline := time.After(timeout)
		for {
			select {
			case env := <-ch:
				if env.ID == id && env.Type == wire.TypeResponse {
					return env
				}
			case <-deadline:
				t.Fatalf("timeout waiting for response %s", id)
			}
		}
	}

	// 1. Owner opens a cursor session to learn the model list.
	ownerWS, ownerCh := dial(claim.Credentials.AccessToken)
	cwd, _ := os.Getwd()
	req, _ := wire.MakeRequest("o-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "cursor-cli", Cwd: cwd})
	_ = wsjson.Write(ctx, ownerWS, req)
	res := awaitResp(ownerCh, "o-1", 90*time.Second)
	if res.Error != nil {
		t.Fatalf("owner create_session: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	_ = json.Unmarshal(res.Payload, &createRes)

	type choice struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	var current string
	var choices []choice
	for _, u := range createRes.PendingUpdates {
		var upd struct {
			Kind    string `json:"sessionUpdate"`
			Options []struct {
				ID           string   `json:"id"`
				CurrentValue string   `json:"currentValue"`
				Options      []choice `json:"options"`
			} `json:"configOptions"`
		}
		if json.Unmarshal(u, &upd) == nil && upd.Kind == "config_option_update" {
			for _, o := range upd.Options {
				if o.ID == "model" {
					current = o.CurrentValue
					choices = o.Options
				}
			}
		}
	}
	if len(choices) == 0 {
		t.Fatal("owner session advertised no models")
	}
	// Pick a cheap, universally-enabled claude model that is NOT the current
	// default so the lock is observable (big "Max" models like fable/opus
	// can be gated by account settings and answer "Check your settings").
	locked := ""
	for _, prefer := range []string{"claude-haiku", "claude-sonnet", "claude"} {
		for _, c := range choices {
			if strings.Contains(c.Value, prefer) && c.Value != current {
				locked = c.Value
				break
			}
		}
		if locked != "" {
			break
		}
	}
	if locked == "" {
		t.Fatalf("no non-default claude model in list (current=%s)", current)
	}
	t.Logf("owner current=%s locking=%s", current, locked)

	// 2. Mint the share with the locked agent+cwd+model.
	body, _ := json.Marshal(map[string]any{
		"ttl_seconds": 600, "prompt_limit": 5, "max_sessions": 1,
		"cwd": cwd, "model": locked, "agent_id": "cursor-cli",
	})
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, hs.URL+"/v1/share/ask", bytes.NewReader(body))
	httpReq.Header.Set("authorization", "Bearer "+claim.Credentials.AccessToken)
	httpReq.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	var share struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&share)
	_ = resp.Body.Close()

	// 3. Guest connects bare and creates a session — relay injects everything.
	guestWS, guestCh := dial(share.Token)
	req, _ = wire.MakeRequest("g-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{})
	_ = wsjson.Write(ctx, guestWS, req)
	res = awaitResp(guestCh, "g-1", 90*time.Second)
	if res.Error != nil {
		t.Fatalf("guest create_session: %+v", res.Error)
	}
	var guestCreate wire.ChatCreateSessionResponse
	_ = json.Unmarshal(res.Payload, &guestCreate)
	if guestCreate.Session.AgentID != "cursor-cli" {
		t.Fatalf("guest agent: %s", guestCreate.Session.AgentID)
	}
	if guestCreate.CurrentModeID != "ask" {
		t.Fatalf("guest mode: %s", guestCreate.CurrentModeID)
	}

	// The daemon must not have logged a model-lock failure.
	rec.mu.Lock()
	for _, w := range rec.warns {
		if strings.Contains(w, "force model") {
			rec.mu.Unlock()
			t.Fatalf("daemon failed to lock the model: %s", w)
		}
	}
	rec.mu.Unlock()

	// Synthetic snapshot should reflect the locked model. cursor may
	// canonicalise the id (param order / defaults), so log a mismatch and
	// rely on the generation check below as the authoritative assertion.
	guestCurrent := ""
	for _, u := range guestCreate.PendingUpdates {
		var upd struct {
			Options []struct {
				ID           string `json:"id"`
				CurrentValue string `json:"currentValue"`
			} `json:"configOptions"`
		}
		if json.Unmarshal(u, &upd) == nil {
			for _, o := range upd.Options {
				if o.ID == "model" {
					guestCurrent = o.CurrentValue
				}
			}
		}
	}
	t.Logf("guest session current model: %q (locked %q)", guestCurrent, locked)
	if guestCurrent != locked {
		t.Logf("note: currentValue differs from locked id — checking generation below")
	}

	// 4. Ask the model who made it — Anthropic models answer reliably, while
	// cursor's own composer answers "Cursor" (verified by probing the CLI
	// directly with -p --model).
	req, _ = wire.MakeRequest("g-2", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{
			SessionID: guestCreate.Session.ID,
			Text:      "Who created you? Answer with only the company name.",
		})
	_ = wsjson.Write(ctx, guestWS, req)
	var answer strings.Builder
	deadline := time.After(120 * time.Second)
	for {
		var env wire.Envelope
		select {
		case env = <-guestCh:
		case <-deadline:
			t.Fatal("timeout waiting for guest prompt")
		}
		if env.Type == wire.TypeEvent && env.Method == wire.ChatEventUpdate {
			var ev wire.ChatUpdateEvent
			_ = json.Unmarshal(env.Payload, &ev)
			var upd struct {
				Kind    string `json:"sessionUpdate"`
				Content struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(ev.Update, &upd) == nil && upd.Kind == "agent_message_chunk" {
				answer.WriteString(upd.Content.Text)
			}
		}
		if env.Type == wire.TypeResponse && env.ID == "g-2" {
			if env.Error != nil {
				t.Fatalf("guest prompt: %+v", env.Error)
			}
			break
		}
	}
	t.Logf("locked=%s creator answer: %q", locked, answer.String())
	if !strings.Contains(strings.ToLower(answer.String()), "anthropic") {
		t.Errorf("expected an Anthropic answer for a locked claude model, got %q", answer.String())
	}
}

// TestLiveCursorModelSwitch covers the owner-side mid-session model switch:
// set_config_option("model") must restart the session's cursor subprocess
// with --model, resume the conversation via session/load (memory intact),
// and generate with the NEW model afterwards.
func TestLiveCursorModelSwitch(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	pc := pairingclient.New(hs.URL)
	init, _ := pc.Init(ctx, "daemon", "lap", "")
	claim, _ := pc.Claim(ctx, init.Code, "client", "phone", "")
	dc, _ := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)

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

	ws, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "") })
	envCh := make(chan wire.Envelope, 256)
	go func() {
		for {
			var env wire.Envelope
			if err := wsjson.Read(ctx, ws, &env); err != nil {
				return
			}
			envCh <- env
		}
	}()

	collectAnswer := func(promptID string, timeout time.Duration) string {
		t.Helper()
		var b strings.Builder
		deadline := time.After(timeout)
		for {
			var env wire.Envelope
			select {
			case env = <-envCh:
			case <-deadline:
				t.Fatalf("timeout waiting for %s", promptID)
			}
			if env.Type == wire.TypeEvent && env.Method == wire.ChatEventUpdate {
				var ev wire.ChatUpdateEvent
				_ = json.Unmarshal(env.Payload, &ev)
				var upd struct {
					Kind    string `json:"sessionUpdate"`
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				if json.Unmarshal(ev.Update, &upd) == nil && upd.Kind == "agent_message_chunk" {
					b.WriteString(upd.Content.Text)
				}
			}
			if env.Type == wire.TypeResponse && env.ID == promptID {
				if env.Error != nil {
					t.Fatalf("%s: %+v", promptID, env.Error)
				}
				return b.String()
			}
		}
	}
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

	// 1. Create a session (default model) and seed a memory.
	cwd, _ := os.Getwd()
	req, _ := wire.MakeRequest("c-1", wire.ChannelChat, wire.ChatMethodCreateSession,
		wire.ChatCreateSessionRequest{AgentID: "cursor-cli", Cwd: cwd})
	_ = wsjson.Write(ctx, ws, req)
	res := awaitResp("c-1", 90*time.Second)
	if res.Error != nil {
		t.Fatalf("create: %+v", res.Error)
	}
	var createRes wire.ChatCreateSessionResponse
	_ = json.Unmarshal(res.Payload, &createRes)
	sid := createRes.Session.ID

	req, _ = wire.MakeRequest("p-1", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: sid, Text: "Remember the secret word: BANANA42. Reply OK."})
	_ = wsjson.Write(ctx, ws, req)
	_ = collectAnswer("p-1", 120*time.Second)

	// 2. Switch the model to a claude variant via the synthetic option.
	var locked string
	for _, u := range createRes.PendingUpdates {
		var upd struct {
			Options []struct {
				ID      string `json:"id"`
				Options []struct {
					Value string `json:"value"`
				} `json:"options"`
			} `json:"configOptions"`
		}
		if json.Unmarshal(u, &upd) == nil {
			for _, o := range upd.Options {
				if o.ID != "model" {
					continue
				}
				for _, c := range o.Options {
					if strings.Contains(c.Value, "claude-haiku") {
						locked = c.Value
					}
				}
			}
		}
	}
	if locked == "" {
		t.Fatal("no claude-haiku model advertised")
	}
	req, _ = wire.MakeRequest("m-1", wire.ChannelChat, wire.ChatMethodSetConfigOption,
		wire.ChatSetConfigOptionRequest{SessionID: sid, ConfigID: "model", Value: locked})
	_ = wsjson.Write(ctx, ws, req)
	res = awaitResp("m-1", 90*time.Second)
	if res.Error != nil {
		t.Fatalf("set_config_option: %+v", res.Error)
	}
	t.Logf("switched to %s", locked)

	// 3. Memory must survive the restart AND generation must use the new
	// model (Anthropic answers; composer answers "Cursor").
	req, _ = wire.MakeRequest("p-2", wire.ChannelChat, wire.ChatMethodPrompt,
		wire.ChatPromptRequest{SessionID: sid,
			Text: "What was the secret word? And who created you? Answer exactly: WORD / COMPANY."})
	_ = wsjson.Write(ctx, ws, req)
	answer := collectAnswer("p-2", 120*time.Second)
	t.Logf("answer after switch: %q", answer)
	low := strings.ToLower(answer)
	if !strings.Contains(low, "banana42") {
		t.Errorf("conversation memory lost across model switch: %q", answer)
	}
	if !strings.Contains(low, "anthropic") {
		t.Errorf("expected Anthropic after switching to %s, got %q", locked, answer)
	}
}
