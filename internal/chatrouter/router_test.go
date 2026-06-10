package chatrouter

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/Mutech-Innovation/coding-anywhere/internal/agent/acpagent"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

// TestAttachmentsToContentBlocks_FieldNames pins the ACP field-naming
// contract (camelCase, not snake_case). Devin's ACP server rejects
// `mime_type` with `Invalid params` / `missing field "mimeType"` so the
// router must translate the wire-side snake_case `mime_type` JSON tag
// into camelCase on the way out.
func TestAttachmentsToContentBlocks_FieldNames(t *testing.T) {
	r := &Router{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	blocks := r.attachmentsToContentBlocks("hi", []wire.Attachment{
		{Type: "image", Name: "shot.png", Data: "AAA=", MimeType: "image/png"},
		{Type: "file", Name: "draft.md", URI: "file:///tmp/draft.md", MimeType: "text/markdown"},
	})

	// Block 0: the text content.
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks (text + image + resource_link), got %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "hi" {
		t.Fatalf("block 0 should be the text turn: %+v", blocks[0])
	}

	// Marshal the image block and the resource_link block end-to-end —
	// `attachmentsToContentBlocks` produces a ContentBlock whose Extra
	// map is supposed to be flattened by ContentBlock.MarshalJSON, so
	// asserting on the wire bytes is the only thing that catches a
	// snake_case regression.
	imgRaw, err := json.Marshal(blocks[1])
	if err != nil {
		t.Fatalf("marshal image: %v", err)
	}
	if !strings.Contains(string(imgRaw), `"mimeType":"image/png"`) {
		t.Fatalf("image block should carry mimeType (camelCase); got %s", string(imgRaw))
	}
	if strings.Contains(string(imgRaw), "mime_type") {
		t.Fatalf("image block must NOT carry snake_case mime_type; got %s", string(imgRaw))
	}
	if !strings.Contains(string(imgRaw), `"data":"AAA="`) {
		t.Fatalf("image block missing data: %s", string(imgRaw))
	}

	linkRaw, err := json.Marshal(blocks[2])
	if err != nil {
		t.Fatalf("marshal resource_link: %v", err)
	}
	if !strings.Contains(string(linkRaw), `"mimeType":"text/markdown"`) {
		t.Fatalf("resource_link block should carry mimeType (camelCase); got %s", string(linkRaw))
	}
	if strings.Contains(string(linkRaw), "mime_type") {
		t.Fatalf("resource_link block must NOT carry snake_case mime_type; got %s", string(linkRaw))
	}
	if !strings.Contains(string(linkRaw), `"uri":"file:///tmp/draft.md"`) {
		t.Fatalf("resource_link block missing uri: %s", string(linkRaw))
	}
	if !strings.Contains(string(linkRaw), `"name":"draft.md"`) {
		t.Fatalf("resource_link block missing name: %s", string(linkRaw))
	}
}

// TestAttachmentsToContentBlocks_DropsInvalidImage covers an image
// attachment that arrives without data or mimeType. Such a block would
// be rejected by Devin's ACP server (`missing field "data"` /
// `missing field "mimeType"`) and tank the entire prompt, so we drop
// it rather than fail the turn.
func TestAttachmentsToContentBlocks_DropsInvalidImage(t *testing.T) {
	r := &Router{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	cases := []wire.Attachment{
		{Type: "image", Name: "noop.png"},                           // no data, no mime
		{Type: "image", Name: "no-mime.png", Data: "AAA="},          // no mime
		{Type: "image", Name: "no-data.png", MimeType: "image/png"}, // no data
	}
	for _, att := range cases {
		blocks := r.attachmentsToContentBlocks("hi", []wire.Attachment{att})
		// Only the text block should survive.
		if len(blocks) != 1 || blocks[0].Type != "text" {
			t.Fatalf("%s: expected only the text block, got %+v", att.Name, blocks)
		}
	}
}

// TestAttachmentsToContentBlocks_TextOnly covers a plain prompt without
// attachments — should produce exactly one text block.
func TestAttachmentsToContentBlocks_TextOnly(t *testing.T) {
	r := &Router{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	blocks := r.attachmentsToContentBlocks("just text", nil)
	if len(blocks) != 1 || blocks[0].Type != "text" || blocks[0].Text != "just text" {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

// TestModelSpawnCandidates pins the decorated-id → cursor `--model` flag
// translation used when a spawn dies with "Cannot use this model".
func TestModelSpawnCandidates(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{
			in: "claude-fable-5[thinking=true,context=300k,effort=high]",
			want: []string{
				"claude-fable-5[thinking=true,context=300k,effort=high]",
				"claude-fable-5-thinking-high",
				"claude-fable-5",
			},
		},
		{
			in: "gpt-5.5[context=272k,reasoning=medium,fast=false]",
			want: []string{
				"gpt-5.5[context=272k,reasoning=medium,fast=false]",
				"gpt-5.5-medium",
				"gpt-5.5",
			},
		},
		{
			in: "claude-opus-4-8[thinking=true,effort=high,fast=true]",
			want: []string{
				"claude-opus-4-8[thinking=true,effort=high,fast=true]",
				"claude-opus-4-8-thinking-high-fast",
				"claude-opus-4-8",
			},
		},
		// No brackets → no fallbacks.
		{in: "composer-2.5", want: []string{"composer-2.5"}},
		// Empty params → translated == base, deduped.
		{in: "default[]", want: []string{"default[]", "default"}},
		{in: "", want: []string{""}},
	}
	for _, tc := range cases {
		got := modelSpawnCandidates(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
			}
		}
	}
}

// TestBuildAdapterForSessionShortCircuitsAuthFailure pins that the model-id
// fallback loop stops after the first authenticate failure. Auth failures
// (bad credentials, or cursor's auth backend unreachable — it needs the
// network even when already logged in) hit every candidate identically and
// cost ~20s each, so retrying other --model spellings only delays the error.
func TestBuildAdapterForSessionShortCircuitsAuthFailure(t *testing.T) {
	attempts := 0
	r := &Router{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		builders: Builders{
			AgentCursor: func(acpagent.Options) (*acpagent.Adapter, error) {
				attempts++
				return nil, &acpagent.AuthFailedError{
					Err: errors.New("authenticate (cursor_login): rpc error -32603: Internal error"),
				}
			},
		},
	}

	_, err := r.buildAdapterForSession(AgentCursor, "composer-2.5[fast=true]")
	var authErr *acpagent.AuthFailedError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthFailedError, got %T %v", err, err)
	}
	if attempts != 1 {
		t.Fatalf("auth failure should stop the fallback loop after 1 attempt, got %d", attempts)
	}
}

// TestBuildAdapterForSessionRetriesNonAuthFailure pins the inverse: a spawn
// failure that is NOT an auth failure (e.g. the process dying on an
// unparseable --model id) still walks the full candidate list.
func TestBuildAdapterForSessionRetriesNonAuthFailure(t *testing.T) {
	attempts := 0
	r := &Router{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		builders: Builders{
			AgentCursor: func(acpagent.Options) (*acpagent.Adapter, error) {
				attempts++
				return nil, errors.New("initialize: EOF (stderr: Cannot use this model)")
			},
		},
	}

	_, err := r.buildAdapterForSession(AgentCursor, "composer-2.5[fast=true]")
	if err == nil {
		t.Fatal("expected error")
	}
	// "composer-2.5[fast=true]" expands to itself + "composer-2.5-fast" +
	// "composer-2.5" (see modelSpawnCandidates).
	if attempts != 3 {
		t.Fatalf("expected all 3 candidates tried, got %d", attempts)
	}
}
