package portsapi_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/portsapi"
)

func TestListListeningSeesOurOwnSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	port := uint16(ln.Addr().(*net.TCPAddr).Port)

	api := portsapi.New()
	// Tiny retry loop because gopsutil snapshots the kernel state and may
	// briefly miss a freshly-opened socket on some kernels.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ports, err := api.ListListening(context.Background())
		if err != nil {
			t.Fatalf("ListListening: %v", err)
		}
		for _, p := range ports {
			if p.Port == port {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("port %d not seen by ListListening", port)
}

func TestFetchProxiesToLocalhost(t *testing.T) {
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "yes")
		_, _ = io.WriteString(w, "hello "+r.URL.Path)
	}))
	t.Cleanup(hs.Close)
	port := portFromURL(t, hs.URL)

	api := portsapi.New()
	res, err := api.Fetch(context.Background(), portsapi.FetchRequest{
		Port:   port,
		Method: "GET",
		Path:   "/world",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Status != 200 {
		t.Fatalf("status: %d", res.Status)
	}
	if res.Headers.Get("X-Test") != "yes" {
		t.Fatalf("missing custom header: %v", res.Headers)
	}
	if string(res.Body) != "hello /world" {
		t.Fatalf("body: %q", res.Body)
	}
}

func TestFetchTruncatesAtMaxBytes(t *testing.T) {
	big := strings.Repeat("a", 10*1024)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, big)
	}))
	t.Cleanup(hs.Close)
	port := portFromURL(t, hs.URL)

	api := portsapi.New()
	res, err := api.Fetch(context.Background(), portsapi.FetchRequest{
		Port: port, Path: "/", MaxBodyBytes: 100,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.Truncated || len(res.Body) != 100 {
		t.Fatalf("truncation: truncated=%v len=%d", res.Truncated, len(res.Body))
	}
}

func portFromURL(t *testing.T, raw string) uint16 {
	t.Helper()
	// Crude parse — httptest URLs always look like http://127.0.0.1:NNN.
	parts := strings.Split(raw, ":")
	last := parts[len(parts)-1]
	var p uint16
	if _, err := fmt.Sscanf(last, "%d", &p); err != nil {
		t.Fatalf("port parse: %v", err)
	}
	return p
}
