//go:build cursorlive

package chatrouter_test

// Temporary live-fire test against the real `cursor-agent acp` binary.
// Run: go test -tags cursorlive ./internal/chatrouter/ -run TestLiveCursor -v
// Requires: cursor-agent on PATH + `cursor-agent login` done.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"
	"github.com/Mutech-Innovation/coding-anywhere/internal/chatrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
)

func TestLiveCursor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	var updates []string
	a, err := chatrouter.DefaultCursorBuilder(acpagent.Options{
		Logger: logger,
		OnSessionUpdate: func(sid string, update json.RawMessage) {
			var head struct {
				Kind string `json:"sessionUpdate"`
			}
			_ = json.Unmarshal(update, &head)
			updates = append(updates, head.Kind)
			if head.Kind == "agent_message_chunk" {
				fmt.Printf("[update] %s: %s\n", head.Kind, string(update))
			} else {
				fmt.Printf("[update] %s\n", head.Kind)
			}
		},
	})
	if err != nil {
		t.Fatalf("builder: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start (initialize+authenticate): %v", err)
	}
	t.Log("initialize + authenticate OK")

	cwd, _ := os.Getwd() // repo dir = a real project
	res, err := a.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Logf("session=%s", res.SessionID)
	if res.Modes == nil || len(res.Modes.AvailableModes) == 0 {
		t.Fatalf("expected modes, got %+v", res.Modes)
	}
	t.Logf("modes: current=%s n=%d", res.Modes.CurrentModeID, len(res.Modes.AvailableModes))
	if res.Models == nil || len(res.Models.AvailableModels) == 0 {
		t.Fatalf("expected models, got %+v", res.Models)
	}
	t.Logf("models: current=%s n=%d first=%s",
		res.Models.CurrentModelID, len(res.Models.AvailableModels),
		res.Models.AvailableModels[0].ModelID)

	// Force ask mode like a guest share would.
	if err := a.SetMode(ctx, res.SessionID, "ask"); err != nil {
		t.Fatalf("SetMode(ask): %v", err)
	}
	t.Log("set_mode ask OK")

	// Switch the model through the cursor-native path.
	target := ""
	for _, m := range res.Models.AvailableModels {
		if m.ModelID != res.Models.CurrentModelID {
			target = m.ModelID
			break
		}
	}
	if target != "" {
		if err := a.SetModel(ctx, res.SessionID, target); err != nil {
			t.Fatalf("SetModel(%s): %v", target, err)
		}
		t.Logf("set_model %s OK", target)
	}

	stop, err := a.PromptText(ctx, res.SessionID, "Reply with exactly: PONG")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	t.Logf("stopReason=%s updates=%v", stop, updates)
	if stop != "end_turn" {
		t.Fatalf("stopReason: %s", stop)
	}
	saw := strings.Join(updates, ",")
	if !strings.Contains(saw, "agent_message_chunk") {
		t.Fatalf("no agent_message_chunk seen: %s", saw)
	}
}

// TestLiveCursorFableFallback reproduces the exact share-link failure: the
// decorated id of claude-fable-5 is rejected by cursor's --model parser (the
// process prints "Cannot use this model" and exits during the handshake).
// buildAdapterForSession must fall back to the translated flag spelling and
// produce a working session.
func TestLiveCursorFableFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cli := relayclient.New(relayclient.Config{RelayURL: "ws://127.0.0.1:1", AccessToken: "x"}, logger)
	r := chatrouter.New(cli, logger, nil)
	t.Cleanup(func() { _ = r.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a, err := chatrouter.BuildAdapterForSessionForTest(r,
		chatrouter.AgentCursor, "claude-fable-5[thinking=true,context=300k,effort=high]")
	if err != nil {
		t.Fatalf("fallback spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	cwd, _ := os.Getwd()
	res, err := a.NewSession(ctx, cwd, nil)
	if err != nil {
		t.Fatalf("NewSession on fallback process: %v", err)
	}
	t.Logf("session=%s current=%v", res.SessionID, res.Models != nil)
}
