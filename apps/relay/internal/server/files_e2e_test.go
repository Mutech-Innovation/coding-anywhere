package server_test

// End-to-end exercise of the `files` channel: pair daemon + client, mount
// a temp workspace on the daemon, list it through the relay, read a known
// file, verify the result.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/filesrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestFilesEndToEnd(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Workspace seeded with two files.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("# Hello\nThis is a test.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "pkg", "main.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pair.
	pc := pairingclient.New(hs.URL)
	init, err := pc.Init(ctx, "daemon", "lap", "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := pc.Claim(ctx, init.Code, "client", "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	dc, err := pc.WaitClaimed(ctx, init.PairingToken, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	// Daemon-side relay client + filesrouter.
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	if _, err := filesrouter.New(dcli, logger, ws); err != nil {
		t.Fatalf("filesrouter: %v", err)
	}
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	// Client raw WS.
	clientWS, _, err := websocket.Dial(ctx, relayURL+"/v1/ws/connect?token="+claim.Credentials.AccessToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientWS.Close(websocket.StatusNormalClosure, "") })

	send := func(id, method string, payload any) {
		req, err := wire.MakeRequest(id, wire.ChannelFiles, method, payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Write(ctx, clientWS, req); err != nil {
			t.Fatal(err)
		}
	}
	awaitRes := func(id string) wire.Envelope {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case <-deadline:
				t.Fatal("timeout for", id)
			default:
				var env wire.Envelope
				if err := wsjson.Read(ctx, clientWS, &env); err != nil {
					t.Fatalf("read: %v", err)
				}
				if env.ID == id && env.Type == wire.TypeResponse {
					return env
				}
			}
		}
	}

	// list_workspace
	send("ws-1", wire.FilesMethodListWorkspace, struct{}{})
	res := awaitRes("ws-1")
	var lwr wire.FilesListWorkspaceResponse
	if err := json.Unmarshal(res.Payload, &lwr); err != nil {
		t.Fatal(err)
	}
	if len(lwr.Workspaces) != 1 || lwr.Workspaces[0].ID != "default" {
		t.Fatalf("workspace: %+v", lwr)
	}
	// Use EvalSymlinks because t.TempDir on macOS lives under /var/folders
	// (a symlink to /private/var/folders/...).
	wsResolved, _ := filepath.EvalSymlinks(ws)
	if lwr.Workspaces[0].Root != wsResolved {
		t.Fatalf("workspace root: got %q want %q", lwr.Workspaces[0].Root, wsResolved)
	}

	// list root
	send("l-1", wire.FilesMethodList, wire.FilesListRequest{Path: ""})
	res = awaitRes("l-1")
	var lr wire.FilesListResponse
	if err := json.Unmarshal(res.Payload, &lr); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range lr.Entries {
		names = append(names, e.Name)
	}
	if !contains(names, "README.md") || !contains(names, "pkg") {
		t.Fatalf("entries missing: %+v", names)
	}

	// list pkg
	send("l-2", wire.FilesMethodList, wire.FilesListRequest{Path: "pkg"})
	res = awaitRes("l-2")
	if err := json.Unmarshal(res.Payload, &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Entries) != 1 || lr.Entries[0].Name != "main.go" {
		t.Fatalf("pkg listing: %+v", lr.Entries)
	}

	// read README
	send("r-1", wire.FilesMethodRead, wire.FilesReadRequest{Path: "README.md"})
	res = awaitRes("r-1")
	if res.Error != nil {
		t.Fatalf("read error: %+v", res.Error)
	}
	var rr wire.FilesReadResponse
	if err := json.Unmarshal(res.Payload, &rr); err != nil {
		t.Fatal(err)
	}
	bytes, err := base64.StdEncoding.DecodeString(rr.Data)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if !strings.Contains(string(bytes), "Hello") {
		t.Fatalf("read result: %q", bytes)
	}

	// list ../etc → forbidden
	send("l-3", wire.FilesMethodList, wire.FilesListRequest{Path: "../etc"})
	res = awaitRes("l-3")
	if res.Error == nil || res.Error.Code != "forbidden" {
		t.Fatalf("expected forbidden, got %+v", res.Error)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
