package chatrouter

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

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
		{Type: "image", Name: "noop.png"},                            // no data, no mime
		{Type: "image", Name: "no-mime.png", Data: "AAA="},           // no mime
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
