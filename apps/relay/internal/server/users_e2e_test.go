package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

// credBundle is the subset of the relay's CredentialBundle the user tests
// care about.
type credBundle struct {
	AccountID   string `json:"account_id"`
	AccessToken string `json:"access_token"`
	DeviceKind  string `json:"device_kind"`
}

// pairAccountForUserTest runs init(daemon)+claim(client) to create a fresh
// account and returns a client access token plus the account id. Each call
// makes a brand-new account (no Bearer), so it doubles as "another machine".
func pairAccountForUserTest(t *testing.T, baseURL, daemonLabel string) (accessToken, accountID string) {
	t.Helper()
	initRes := postJSON(t, baseURL+"/v1/pair/init", map[string]string{
		"device_kind": "daemon", "device_label": daemonLabel,
	})
	defer closeBody(initRes.Body)
	var initBody struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(initRes.Body).Decode(&initBody); err != nil {
		t.Fatalf("decode init: %v", err)
	}
	claimRes := postJSON(t, baseURL+"/v1/pair/claim", map[string]string{
		"code": initBody.Code, "device_kind": "client", "device_label": "browser",
	})
	defer closeBody(claimRes.Body)
	if claimRes.StatusCode != 200 {
		t.Fatalf("claim status: %d", claimRes.StatusCode)
	}
	var claimBody struct {
		Credentials credBundle `json:"credentials"`
	}
	if err := json.NewDecoder(claimRes.Body).Decode(&claimBody); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	return claimBody.Credentials.AccessToken, claimBody.Credentials.AccountID
}

func postJSONAuth(t *testing.T, url string, body any, bearer string) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func getAuth(t *testing.T, url, bearer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return res
}

func registerUnder(t *testing.T, baseURL, bearer, username, password string) {
	t.Helper()
	res := postJSONAuth(t, baseURL+"/v1/auth/register", map[string]string{
		"username": username, "password": password,
	}, bearer)
	defer closeBody(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("register status: %d", res.StatusCode)
	}
}

func TestUserMachinesAndSwitch(t *testing.T) {
	hs, _ := newPairingTestServer(t)

	// Two machines under one user, plus a third under a different user.
	bearerA, accA := pairAccountForUserTest(t, hs.URL, "work-laptop")
	bearerB, accB := pairAccountForUserTest(t, hs.URL, "home-desktop")
	bearerC, accC := pairAccountForUserTest(t, hs.URL, "someone-else")
	registerUnder(t, hs.URL, bearerA, "owner", "supersecret")
	registerUnder(t, hs.URL, bearerB, "owner", "supersecret")
	registerUnder(t, hs.URL, bearerC, "stranger", "supersecret")

	// machines from A lists both of owner's machines, current = A.
	mRes := getAuth(t, hs.URL+"/v1/auth/user/machines", bearerA)
	defer closeBody(mRes.Body)
	if mRes.StatusCode != 200 {
		t.Fatalf("machines status: %d", mRes.StatusCode)
	}
	var machines struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
			Label     string `json:"label"`
		} `json:"accounts"`
		Current string `json:"current"`
	}
	if err := json.NewDecoder(mRes.Body).Decode(&machines); err != nil {
		t.Fatalf("decode machines: %v", err)
	}
	if machines.Current != accA || len(machines.Accounts) != 2 {
		t.Fatalf("expected 2 machines current=%s, got current=%s n=%d", accA, machines.Current, len(machines.Accounts))
	}

	// Switch A → B yields credentials for B.
	sRes := postJSONAuth(t, hs.URL+"/v1/auth/user/switch", map[string]string{
		"account_id": accB, "device_label": "chrome",
	}, bearerA)
	defer closeBody(sRes.Body)
	if sRes.StatusCode != 200 {
		t.Fatalf("switch status: %d", sRes.StatusCode)
	}
	var sw struct {
		Current     string      `json:"current"`
		Credentials *credBundle `json:"credentials"`
	}
	if err := json.NewDecoder(sRes.Body).Decode(&sw); err != nil {
		t.Fatalf("decode switch: %v", err)
	}
	if sw.Current != accB || sw.Credentials == nil || sw.Credentials.AccountID != accB {
		t.Fatalf("expected switch to %s, got current=%s creds=%+v", accB, sw.Current, sw.Credentials)
	}

	// Cannot switch to a machine you don't own (accC belongs to "stranger").
	badRes := postJSONAuth(t, hs.URL+"/v1/auth/user/switch", map[string]string{
		"account_id": accC,
	}, bearerA)
	defer closeBody(badRes.Body)
	if badRes.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 switching to a stranger's machine, got %d", badRes.StatusCode)
	}

	// The stranger only sees their own machine.
	cRes := getAuth(t, hs.URL+"/v1/auth/user/machines", bearerC)
	defer closeBody(cRes.Body)
	var cm struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(cRes.Body).Decode(&cm); err != nil {
		t.Fatalf("decode stranger machines: %v", err)
	}
	if len(cm.Accounts) != 1 || cm.Accounts[0].AccountID != accC {
		t.Fatalf("stranger should see only their machine, got %+v", cm.Accounts)
	}
}

func TestRegisterRequiresBearer(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	res := postJSON(t, hs.URL+"/v1/auth/register", map[string]string{
		"username": "alice", "password": "supersecret",
	})
	defer closeBody(res.Body)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without bearer, got %d", res.StatusCode)
	}
}

func TestRegisterAndUserLoginSingleAccount(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	bearer, accountID := pairAccountForUserTest(t, hs.URL, "laptop")

	// Register a user and link this account.
	regRes := postJSONAuth(t, hs.URL+"/v1/auth/register", map[string]string{
		"username": "owner", "password": "supersecret",
	}, bearer)
	defer closeBody(regRes.Body)
	if regRes.StatusCode != 200 {
		t.Fatalf("register status: %d", regRes.StatusCode)
	}
	var regBody struct {
		UserID          string `json:"user_id"`
		LinkedAccountID string `json:"linked_account_id"`
	}
	if err := json.NewDecoder(regRes.Body).Decode(&regBody); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	if regBody.LinkedAccountID != accountID {
		t.Fatalf("linked account mismatch: got %q want %q", regBody.LinkedAccountID, accountID)
	}

	// Log in with username/password — single account → straight in.
	loginRes := postJSON(t, hs.URL+"/v1/auth/user/login", map[string]string{
		"username": "owner", "password": "supersecret", "device_label": "chrome",
	})
	defer closeBody(loginRes.Body)
	if loginRes.StatusCode != 200 {
		t.Fatalf("login status: %d", loginRes.StatusCode)
	}
	var loginBody struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
			Label     string `json:"label"`
		} `json:"accounts"`
		Credentials *credBundle `json:"credentials"`
	}
	if err := json.NewDecoder(loginRes.Body).Decode(&loginBody); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginBody.Credentials == nil {
		t.Fatal("expected credentials on single-account login")
	}
	if loginBody.Credentials.AccountID != accountID || loginBody.Credentials.DeviceKind != "client" {
		t.Fatalf("unexpected credentials: %+v", loginBody.Credentials)
	}
	if len(loginBody.Accounts) != 1 || loginBody.Accounts[0].Label != "laptop" {
		t.Fatalf("unexpected accounts list: %+v", loginBody.Accounts)
	}

	// Wrong password is rejected.
	badRes := postJSON(t, hs.URL+"/v1/auth/user/login", map[string]string{
		"username": "owner", "password": "WRONG", "device_label": "chrome",
	})
	defer closeBody(badRes.Body)
	if badRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password, got %d", badRes.StatusCode)
	}
}

func TestUserLoginMultiAccountPicker(t *testing.T) {
	hs, _ := newPairingTestServer(t)

	// Two machines, two accounts, both attached to the same user.
	bearerA, accA := pairAccountForUserTest(t, hs.URL, "work-laptop")
	bearerB, accB := pairAccountForUserTest(t, hs.URL, "home-desktop")

	for _, b := range []string{bearerA, bearerB} {
		res := postJSONAuth(t, hs.URL+"/v1/auth/register", map[string]string{
			"username": "owner", "password": "supersecret",
		}, b)
		if res.StatusCode != 200 {
			t.Fatalf("register status: %d", res.StatusCode)
		}
		closeBody(res.Body)
	}

	// No account_id + multiple accounts → list, no credentials.
	listRes := postJSON(t, hs.URL+"/v1/auth/user/login", map[string]string{
		"username": "owner", "password": "supersecret", "device_label": "chrome",
	})
	defer closeBody(listRes.Body)
	var listBody struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
			Label     string `json:"label"`
		} `json:"accounts"`
		Credentials *credBundle `json:"credentials"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listBody.Credentials != nil {
		t.Fatal("expected NO credentials when multiple accounts and no choice")
	}
	if len(listBody.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(listBody.Accounts))
	}

	// Choose account B → credentials for B.
	pickRes := postJSON(t, hs.URL+"/v1/auth/user/login", map[string]string{
		"username": "owner", "password": "supersecret", "device_label": "chrome", "account_id": accB,
	})
	defer closeBody(pickRes.Body)
	var pickBody struct {
		Credentials *credBundle `json:"credentials"`
	}
	if err := json.NewDecoder(pickRes.Body).Decode(&pickBody); err != nil {
		t.Fatalf("decode pick: %v", err)
	}
	if pickBody.Credentials == nil || pickBody.Credentials.AccountID != accB {
		t.Fatalf("expected credentials for account B (%s), got %+v", accB, pickBody.Credentials)
	}
	_ = accA // accA is exercised via the list above
}

func TestRegisterExistingUsernameWrongPasswordRejected(t *testing.T) {
	hs, _ := newPairingTestServer(t)
	bearerA, _ := pairAccountForUserTest(t, hs.URL, "laptop-a")
	bearerB, _ := pairAccountForUserTest(t, hs.URL, "laptop-b")

	// First account registers the username.
	res := postJSONAuth(t, hs.URL+"/v1/auth/register", map[string]string{
		"username": "owner", "password": "supersecret",
	}, bearerA)
	if res.StatusCode != 200 {
		t.Fatalf("register A status: %d", res.StatusCode)
	}
	closeBody(res.Body)

	// Second account tries to attach with the wrong password → 401.
	res2 := postJSONAuth(t, hs.URL+"/v1/auth/register", map[string]string{
		"username": "owner", "password": "not-the-password",
	}, bearerB)
	defer closeBody(res2.Body)
	if res2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 attaching with wrong password, got %d", res2.StatusCode)
	}
}
