package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/pairingclient"
)

func TestVAPIDPublicKeyIsStable(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	res, err := http.Get(hs.URL + "/v1/push/vapid_public_key")
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	var body struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.PublicKey) < 64 {
		t.Fatalf("public key too short: %q", body.PublicKey)
	}

	// Calling again should return the same key (persisted).
	res2, err := http.Get(hs.URL + "/v1/push/vapid_public_key")
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(res2.Body)
	var body2 struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(res2.Body).Decode(&body2); err != nil {
		t.Fatal(err)
	}
	if body.PublicKey != body2.PublicKey {
		t.Fatal("vapid public key changed across calls")
	}
}

func TestPushSubscribeRequiresAuth(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	res, err := http.Post(hs.URL+"/v1/push/subscribe", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(res.Body)
	if res.StatusCode != 401 {
		t.Fatalf("status: %d", res.StatusCode)
	}
}

func TestPushSubscribeAndUnsubscribeRoundTrip(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Pair to get client credentials.
	pc := pairingclient.New(hs.URL)
	init, err := pc.Init(ctx, "daemon", "lap", "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := pc.Claim(ctx, init.Code, "client", "phone", "")
	if err != nil {
		t.Fatal(err)
	}
	tok := claim.Credentials.AccessToken

	// Subscribe with a fake browser endpoint. The relay just persists; it
	// won't try to dispatch unless we ask it to.
	subBody := `{
        "endpoint": "https://push.example.com/abcd1234",
        "keys": {"p256dh":"BNd...","auth":"abc"},
        "user_agent": "test-agent"
    }`
	req, _ := http.NewRequestWithContext(ctx, "POST", hs.URL+"/v1/push/subscribe", strings.NewReader(subBody))
	req.Header.Set("authorization", "Bearer "+tok)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(res.Body)
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("subscribe: %d %s", res.StatusCode, body)
	}

	// Test endpoint should return delivered=0 failures>=0 (the push.example.com
	// endpoint will error out, but the relay shouldn't 500).
	tReq, _ := http.NewRequestWithContext(ctx, "POST", hs.URL+"/v1/push/test", strings.NewReader(`{}`))
	tReq.Header.Set("authorization", "Bearer "+tok)
	tReq.Header.Set("content-type", "application/json")
	tRes, err := http.DefaultClient.Do(tReq)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(tRes.Body)
	if tRes.StatusCode != 200 {
		t.Fatalf("test status: %d", tRes.StatusCode)
	}

	// Unsubscribe.
	uReq, _ := http.NewRequestWithContext(ctx, "POST", hs.URL+"/v1/push/unsubscribe",
		strings.NewReader(`{"endpoint":"https://push.example.com/abcd1234"}`))
	uReq.Header.Set("authorization", "Bearer "+tok)
	uReq.Header.Set("content-type", "application/json")
	uRes, err := http.DefaultClient.Do(uReq)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBody(uRes.Body)
	if uRes.StatusCode != 200 {
		t.Fatalf("unsubscribe: %d", uRes.StatusCode)
	}
}
