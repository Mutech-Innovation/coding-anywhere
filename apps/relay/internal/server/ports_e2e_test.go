package server_test

// End-to-end exercise of the relay's HTTP proxy: client hits
// `/v1/proxy/<port>/<path>` on the relay, which forwards via the WS hub
// to the daemon's portsrouter, which fetches against a local httptest
// server. The bytes round-trip back to the client.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/portsrouter"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
)

func TestPortsProxyEndToEnd(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// 1. Fake "user app" served on a real localhost port.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json" && r.Header.Get("X-From-Client") != "" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"echo":%q,"client_header":%q}`, r.URL.Path, r.Header.Get("X-From-Client"))
			return
		}
		fmt.Fprintln(w, "Hello "+r.URL.Path)
	}))
	t.Cleanup(upstream.Close)
	upstreamPort := portFromHTTPTestURL(t, upstream.URL)

	// 2. Pair daemon + client.
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

	// 3. Daemon-side: relay client + ports router.
	relayURL := strings.Replace(hs.URL, "http", "ws", 1)
	dcli := relayclient.New(relayclient.Config{
		RelayURL: relayURL, AccessToken: dc.AccessToken,
		PingInterval: time.Second, HandshakeTimeout: 3 * time.Second,
	}, logger)
	portsrouter.New(dcli, logger)
	go func() { _ = dcli.Run(ctx) }()
	if err := dcli.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}

	// 4. Client hits the relay's proxy endpoint with its access token.
	proxyURL := fmt.Sprintf("%s/v1/proxy/%d/json?token=%s", hs.URL, upstreamPort, claim.Credentials.AccessToken)
	req, err := http.NewRequestWithContext(ctx, "GET", proxyURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-From-Client", "yes-please")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `"echo":"/json"`) {
		t.Fatalf("body: %s", body)
	}
	if !strings.Contains(string(body), `"client_header":"yes-please"`) {
		t.Fatalf("client header not propagated: %s", body)
	}
}

func TestPortsProxyRejectsMissingToken(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	res, err := http.Get(hs.URL + "/v1/proxy/3000/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("status: %d", res.StatusCode)
	}
}

func TestPortsProxyReturnsBadGatewayWhenNoDaemon(t *testing.T) {
	hs, srv := newPairingTestServer(t)
	tok, err := srv.Signer().Issue("acc-1", "client-1", "client", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(fmt.Sprintf("%s/v1/proxy/3000/?token=%s", hs.URL, tok))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 502 {
		t.Fatalf("status: %d (want 502)", res.StatusCode)
	}
}

// portFromHTTPTestURL extracts the listen port from an httptest.Server URL.
func portFromHTTPTestURL(t *testing.T, raw string) uint16 {
	t.Helper()
	parts := strings.Split(raw, ":")
	last := parts[len(parts)-1]
	var p uint16
	if _, err := fmt.Sscanf(last, "%d", &p); err != nil {
		t.Fatalf("port parse: %v", err)
	}
	return p
}
