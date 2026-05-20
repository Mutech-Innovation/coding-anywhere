package ptymux_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/ptymux"
)

// drain reads chunks from ch into a buffer until either the channel is
// closed or `predicate` returns true. Times out at `timeout` and reports
// what it had so far.
func drain(t *testing.T, ch <-chan []byte, predicate func([]byte) bool, timeout time.Duration) []byte {
	t.Helper()
	var buf bytes.Buffer
	deadline := time.After(timeout)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return buf.Bytes()
			}
			buf.Write(chunk)
			if predicate(buf.Bytes()) {
				return buf.Bytes()
			}
		case <-deadline:
			return buf.Bytes()
		}
	}
}

func TestSessionEchoesShellCommand(t *testing.T) {
	mgr := ptymux.NewManager()
	t.Cleanup(func() { _ = mgr.CloseAll() })

	s, err := mgr.Open(ptymux.SessionOptions{
		Command: []string{"/bin/sh"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, _, ch, detach := s.Subscribe(0)
	t.Cleanup(detach)

	// Hand the shell a literal payload to print.
	if _, err := s.Write([]byte("printf coding-anywhere-PTY-OK\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := drain(t, ch, func(b []byte) bool {
		return bytes.Contains(b, []byte("coding-anywhere-PTY-OK"))
	}, 3*time.Second)
	if !bytes.Contains(got, []byte("coding-anywhere-PTY-OK")) {
		t.Fatalf("expected payload in output, got %q", got)
	}
}

func TestSessionResizePropagates(t *testing.T) {
	mgr := ptymux.NewManager()
	t.Cleanup(func() { _ = mgr.CloseAll() })

	s, err := mgr.Open(ptymux.SessionOptions{Command: []string{"/bin/sh"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, _, ch, detach := s.Subscribe(0)
	t.Cleanup(detach)

	if err := s.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if _, err := s.Write([]byte("stty size\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := drain(t, ch, func(b []byte) bool {
		return bytes.Contains(b, []byte("40 120"))
	}, 3*time.Second)
	if !bytes.Contains(got, []byte("40 120")) {
		t.Fatalf("stty did not report new size; got: %q", got)
	}
	info := s.Info()
	if info.Cols != 120 || info.Rows != 40 {
		t.Fatalf("info size: %dx%d", info.Cols, info.Rows)
	}
}

func TestSessionScrollbackReplay(t *testing.T) {
	mgr := ptymux.NewManager()
	t.Cleanup(func() { _ = mgr.CloseAll() })

	s, err := mgr.Open(ptymux.SessionOptions{
		Command: []string{"/bin/sh"},
		Cols:    80, Rows: 24,
		ScrollbackBytes: 4 << 10,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.Write([]byte("printf 'banner-line-1\\n'\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Wait briefly so the readLoop fans out into the ring buffer.
	time.Sleep(200 * time.Millisecond)

	// New subscriber should get a snapshot containing the line.
	_, snapshot, ch, detach := s.Subscribe(0)
	t.Cleanup(detach)

	if !bytes.Contains(snapshot, []byte("banner-line-1")) {
		t.Fatalf("snapshot missing banner; got %q", snapshot)
	}

	// Subsequent live writes still land on this subscriber.
	if _, err := s.Write([]byte("printf 'live-line-2\\n'\n")); err != nil {
		t.Fatal(err)
	}
	got := drain(t, ch, func(b []byte) bool {
		return bytes.Contains(b, []byte("live-line-2"))
	}, 3*time.Second)
	if !bytes.Contains(got, []byte("live-line-2")) {
		t.Fatalf("live chunk missing; got %q", got)
	}
}

func TestSessionCloseStopsSubscribers(t *testing.T) {
	mgr := ptymux.NewManager()
	s, err := mgr.Open(ptymux.SessionOptions{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, _, ch, detach := s.Subscribe(0)
	t.Cleanup(detach)

	if err := mgr.Close(s.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The subscriber channel must be closed in finite time.
	select {
	case _, ok := <-ch:
		if ok {
			// drain remaining chunks until close.
			for range ch {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber channel not closed after manager.Close")
	}
	if _, err := mgr.Get(s.ID); err == nil {
		t.Fatalf("expected ErrNotFound, got nil")
	}
}

func TestSessionExitWithNonZeroCode(t *testing.T) {
	mgr := ptymux.NewManager()
	t.Cleanup(func() { _ = mgr.CloseAll() })

	s, err := mgr.Open(ptymux.SessionOptions{Command: []string{"/bin/sh", "-c", "exit 42"}})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	exit, err := s.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if exit.Code != 42 {
		t.Fatalf("exit code: %d (want 42)", exit.Code)
	}
}

func TestManagerListIncludesRunningSessions(t *testing.T) {
	mgr := ptymux.NewManager()
	t.Cleanup(func() { _ = mgr.CloseAll() })

	s1, err := mgr.Open(ptymux.SessionOptions{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := mgr.Open(ptymux.SessionOptions{Command: []string{"/bin/sh"}})
	if err != nil {
		t.Fatal(err)
	}
	infos := mgr.List()
	if len(infos) != 2 {
		t.Fatalf("len(infos)=%d", len(infos))
	}
	ids := []string{infos[0].ID, infos[1].ID}
	wantA, wantB := s1.ID, s2.ID
	if !strings.Contains(ids[0]+ids[1], wantA) || !strings.Contains(ids[0]+ids[1], wantB) {
		t.Fatalf("missing ids; got %+v want %s,%s", ids, wantA, wantB)
	}
}
