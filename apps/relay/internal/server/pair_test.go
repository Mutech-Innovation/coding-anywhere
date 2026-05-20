package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/server"
	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
)

// newPairingTestServer is the heavier counterpart to newTestServer that wires
// in a real sqlite store under t.TempDir().
func newPairingTestServer(t *testing.T) (*httptest.Server, *server.Server) {
	t.Helper()
	cfg := server.Defaults()
	cfg.JWTSecret = []byte("0123456789abcdef0123456789abcdef")
	cfg.DataDir = t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(cfg.DataDir, "relay.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv, err := server.NewWithStore(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), st)
	if err != nil {
		t.Fatalf("NewWithStore: %v", err)
	}
	hs := httptest.NewServer(server.HandlerForTest(srv))
	t.Cleanup(hs.Close)
	return hs, srv
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func TestPairFlowHappyPath(t *testing.T) {
	hs, _ := newPairingTestServer(t)

	// 1. Daemon initiates pairing.
	initRes := postJSON(t, hs.URL+"/v1/pair/init", map[string]string{
		"device_kind": "daemon", "device_label": "laptop",
	})
	defer initRes.Body.Close()
	if initRes.StatusCode != 200 {
		t.Fatalf("init status: %d", initRes.StatusCode)
	}
	var initBody struct {
		PairingToken string `json:"pairing_token"`
		Code         string `json:"code"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(initRes.Body).Decode(&initBody); err != nil {
		t.Fatalf("decode init: %v", err)
	}
	if initBody.PairingToken == "" || len(initBody.Code) != 6 || initBody.ExpiresIn <= 0 {
		t.Fatalf("unexpected init body: %+v", initBody)
	}

	// 2. Status before claim is "pending".
	statusRes, err := http.Get(hs.URL + "/v1/pair/status?pairing_token=" + initBody.PairingToken)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	defer statusRes.Body.Close()
	var statusBody struct{ Status string }
	if err := json.NewDecoder(statusRes.Body).Decode(&statusBody); err != nil {
		t.Fatal(err)
	}
	if statusBody.Status != "pending" {
		t.Fatalf("status: %s", statusBody.Status)
	}

	// 3. Web client claims the code.
	claimRes := postJSON(t, hs.URL+"/v1/pair/claim", map[string]string{
		"code": initBody.Code, "device_kind": "client", "device_label": "iphone",
	})
	defer claimRes.Body.Close()
	if claimRes.StatusCode != 200 {
		t.Fatalf("claim status: %d", claimRes.StatusCode)
	}
	var claimBody struct {
		Credentials server.CredentialBundle
	}
	if err := json.NewDecoder(claimRes.Body).Decode(&claimBody); err != nil {
		t.Fatal(err)
	}
	clientCred := claimBody.Credentials
	if clientCred.AccountID == "" || clientCred.DeviceID == "" || clientCred.AccessToken == "" || clientCred.RefreshToken == "" {
		t.Fatalf("client cred missing fields: %+v", clientCred)
	}
	if clientCred.DeviceKind != "client" {
		t.Fatalf("client kind: %s", clientCred.DeviceKind)
	}

	// 4. Daemon polls status, gets daemon credentials.
	statusRes2, err := http.Get(hs.URL + "/v1/pair/status?pairing_token=" + initBody.PairingToken)
	if err != nil {
		t.Fatalf("status #2: %v", err)
	}
	defer statusRes2.Body.Close()
	var statusBody2 struct {
		Status      string
		Credentials *server.CredentialBundle
	}
	if err := json.NewDecoder(statusRes2.Body).Decode(&statusBody2); err != nil {
		t.Fatal(err)
	}
	if statusBody2.Status != "claimed" || statusBody2.Credentials == nil {
		t.Fatalf("unexpected status #2: %+v", statusBody2)
	}
	daemonCred := *statusBody2.Credentials
	if daemonCred.AccountID != clientCred.AccountID {
		t.Fatalf("accounts differ: daemon=%s client=%s", daemonCred.AccountID, clientCred.AccountID)
	}
	if daemonCred.DeviceKind != "daemon" {
		t.Fatalf("daemon kind: %s", daemonCred.DeviceKind)
	}
	if daemonCred.AccessToken == "" || daemonCred.RefreshToken == "" {
		t.Fatalf("daemon cred missing fields")
	}
	if daemonCred.DeviceID == clientCred.DeviceID {
		t.Fatalf("daemon and client device ids should differ")
	}

	// 5. Polling again returns 404 (pairing row deleted).
	statusRes3, err := http.Get(hs.URL + "/v1/pair/status?pairing_token=" + initBody.PairingToken)
	if err != nil {
		t.Fatalf("status #3: %v", err)
	}
	defer statusRes3.Body.Close()
	if statusRes3.StatusCode != 404 {
		t.Fatalf("status #3 code: %d", statusRes3.StatusCode)
	}
}

func TestPairInitValidation(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	cases := []struct {
		name string
		body any
		want int
	}{
		{"missing fields", map[string]string{}, 400},
		{"bad kind", map[string]string{"device_kind": "potato", "device_label": "x"}, 400},
		{"empty label", map[string]string{"device_kind": "daemon", "device_label": "  "}, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := postJSON(t, hs.URL+"/v1/pair/init", tc.body)
			defer res.Body.Close()
			if res.StatusCode != tc.want {
				t.Fatalf("status: got %d want %d", res.StatusCode, tc.want)
			}
		})
	}
}

func TestPairClaimUnknownCode(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	res := postJSON(t, hs.URL+"/v1/pair/claim", map[string]string{
		"code": "999999", "device_kind": "client", "device_label": "phone",
	})
	defer res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("status: %d", res.StatusCode)
	}
}

func TestPairClaimAlreadyClaimed(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	initRes := postJSON(t, hs.URL+"/v1/pair/init", map[string]string{
		"device_kind": "daemon", "device_label": "laptop",
	})
	defer initRes.Body.Close()
	var init struct{ Code string }
	if err := json.NewDecoder(initRes.Body).Decode(&init); err != nil {
		t.Fatal(err)
	}
	first := postJSON(t, hs.URL+"/v1/pair/claim", map[string]string{
		"code": init.Code, "device_kind": "client", "device_label": "phone",
	})
	first.Body.Close()
	if first.StatusCode != 200 {
		t.Fatalf("first claim status: %d", first.StatusCode)
	}
	second := postJSON(t, hs.URL+"/v1/pair/claim", map[string]string{
		"code": init.Code, "device_kind": "client", "device_label": "phone-2",
	})
	defer second.Body.Close()
	if second.StatusCode != 409 {
		t.Fatalf("second claim should be 409, got %d", second.StatusCode)
	}
}

func TestRefreshTokenFlow(t *testing.T) {
	hs, _ := newPairingTestServer(t)

	initRes := postJSON(t, hs.URL+"/v1/pair/init", map[string]string{
		"device_kind": "daemon", "device_label": "laptop",
	})
	defer initRes.Body.Close()
	var init struct{ Code string }
	if err := json.NewDecoder(initRes.Body).Decode(&init); err != nil {
		t.Fatal(err)
	}
	claimRes := postJSON(t, hs.URL+"/v1/pair/claim", map[string]string{
		"code": init.Code, "device_kind": "client", "device_label": "phone",
	})
	defer claimRes.Body.Close()
	var claim struct {
		Credentials server.CredentialBundle
	}
	if err := json.NewDecoder(claimRes.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}

	// 1. Refresh succeeds.
	refreshRes := postJSON(t, hs.URL+"/v1/auth/refresh", map[string]string{
		"refresh_token": claim.Credentials.RefreshToken,
	})
	defer refreshRes.Body.Close()
	if refreshRes.StatusCode != 200 {
		t.Fatalf("refresh: %d", refreshRes.StatusCode)
	}
	var refresh struct {
		AccessToken          string `json:"access_token"`
		AccessTokenExpiresIn int64  `json:"access_token_expires_in"`
	}
	if err := json.NewDecoder(refreshRes.Body).Decode(&refresh); err != nil {
		t.Fatal(err)
	}
	if refresh.AccessToken == "" || refresh.AccessTokenExpiresIn <= 0 {
		t.Fatalf("bad refresh body: %+v", refresh)
	}

	// 2. Bad token → 401.
	bad := postJSON(t, hs.URL+"/v1/auth/refresh", map[string]string{"refresh_token": "totally-fake"})
	bad.Body.Close()
	if bad.StatusCode != 401 {
		t.Fatalf("bad token status: %d", bad.StatusCode)
	}
}

// fast-forward sanity check of pairing expiry: we hand-poke the row to be
// already expired and verify status returns 410 + status="expired".
func TestPairingExpiry(t *testing.T) {
	hs, srv := newPairingTestServer(t)
	initRes := postJSON(t, hs.URL+"/v1/pair/init", map[string]string{
		"device_kind": "daemon", "device_label": "laptop",
	})
	defer initRes.Body.Close()
	var init struct {
		PairingToken string `json:"pairing_token"`
	}
	if err := json.NewDecoder(initRes.Body).Decode(&init); err != nil {
		t.Fatal(err)
	}
	// Force the pairing to be expired in the past.
	_, err := srv.Store().DB().ExecContext(context.Background(),
		`UPDATE pairings SET expires_at = ? WHERE pairing_token = ?`,
		time.Now().Add(-time.Hour).UnixMilli(), init.PairingToken)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	res, err := http.Get(hs.URL + "/v1/pair/status?pairing_token=" + init.PairingToken)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 410 {
		t.Fatalf("status code: %d", res.StatusCode)
	}
	var body struct{ Status string }
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "expired" {
		t.Fatalf("status: %s", body.Status)
	}
}
