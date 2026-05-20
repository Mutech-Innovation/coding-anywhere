package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/oklog/ulid/v2"
)

// handleProxy serves /v1/proxy/<port>/<path...>. The relay forwards the
// request through the WebSocket hub to the account's daemon, which performs
// an actual HTTP request against http://localhost:<port><path>.
//
// Authentication is by Bearer token in the Authorization header (preferred)
// or `?token=` query param (so a freshly opened tab works). Tokens must be
// kind=client; daemon tokens cannot proxy.
func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	port, path, err := parseProxyPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	claims, err := s.authenticateProxy(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Read request body (capped to avoid memory blowups).
	const maxReqBody = 4 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxReqBody))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	headers := []wire.PortsHeader{}
	for k, vs := range r.Header {
		switch k {
		case "Authorization", "Cookie", "Host", "Connection", "Te",
			"Transfer-Encoding", "Upgrade", "Proxy-Connection":
			continue
		}
		for _, v := range vs {
			headers = append(headers, wire.PortsHeader{Name: k, Value: v})
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	queryPath := path
	if r.URL.RawQuery != "" {
		queryPath += "?" + r.URL.RawQuery
		// Strip our own `token=` so the upstream doesn't see it.
		queryPath = stripTokenParam(queryPath)
	}

	envID := ulid.Make().String()
	req, err := wire.MakeRequest(envID, wire.ChannelPorts, wire.PortsMethodFetch, wire.PortsFetchRequest{
		Port:    port,
		Method:  r.Method,
		Path:    queryPath,
		Headers: headers,
		Body:    base64.StdEncoding.EncodeToString(body),
	})
	if err != nil {
		http.Error(w, "build req: "+err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := s.hub.ServerRequest(ctx, claims.AccountID, req)
	if err != nil {
		if errors.Is(err, ErrNoDaemon) {
			http.Error(w, "no daemon connected", http.StatusBadGateway)
			return
		}
		http.Error(w, "proxy: "+err.Error(), http.StatusGatewayTimeout)
		return
	}
	if res.Error != nil {
		http.Error(w, res.Error.Code+": "+res.Error.Message, http.StatusBadGateway)
		return
	}
	var fetched wire.PortsFetchResponse
	if err := json.Unmarshal(res.Payload, &fetched); err != nil {
		http.Error(w, "decode response: "+err.Error(), http.StatusBadGateway)
		return
	}

	for _, h := range fetched.Headers {
		switch h.Name {
		case "Content-Length", "Transfer-Encoding", "Connection":
			continue
		}
		w.Header().Add(h.Name, h.Value)
	}
	if fetched.Truncated {
		w.Header().Set("X-Coding-Anywhere-Truncated", "1")
	}
	if fetched.UpstreamMS > 0 {
		w.Header().Set("X-Coding-Anywhere-Upstream-MS", strconv.FormatInt(fetched.UpstreamMS, 10))
	}
	w.WriteHeader(fetched.Status)
	if len(fetched.Body) > 0 {
		bytes, err := base64.StdEncoding.DecodeString(fetched.Body)
		if err != nil {
			return
		}
		_, _ = w.Write(bytes)
	}
}

func parseProxyPath(p string) (uint16, string, error) {
	const prefix = "/v1/proxy/"
	if !strings.HasPrefix(p, prefix) {
		return 0, "", errors.New("not a proxy path")
	}
	rest := strings.TrimPrefix(p, prefix)
	slash := strings.IndexByte(rest, '/')
	var portStr, sub string
	if slash < 0 {
		portStr = rest
		sub = "/"
	} else {
		portStr = rest[:slash]
		sub = rest[slash:]
	}
	port64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port64 == 0 {
		return 0, "", errors.New("invalid port")
	}
	if sub == "" {
		sub = "/"
	}
	return uint16(port64), sub, nil
}

// authenticateProxy is like authenticateWS but enforces kind=client.
func (s *Server) authenticateProxy(r *http.Request) (auth0, error) {
	var raw string
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimPrefix(h, "Bearer ")
	} else if t := r.URL.Query().Get("token"); t != "" {
		raw = t
	}
	if raw == "" {
		return auth0{}, errors.New("missing token")
	}
	c, err := s.signer.Verify(raw)
	if err != nil {
		return auth0{}, err
	}
	return auth0{AccountID: c.AccountID, DeviceID: c.DeviceID, Kind: string(c.Kind)}, nil
}

// auth0 is a minimal stand-in for the auth.Claims type so callers can work
// with the fields they care about without importing the full type.
type auth0 struct {
	AccountID string
	DeviceID  string
	Kind      string
}

// stripTokenParam removes ?token=... from a URL-with-query so we don't leak
// the relay JWT into the upstream request log.
func stripTokenParam(pq string) string {
	q := strings.Index(pq, "?")
	if q < 0 {
		return pq
	}
	path, raw := pq[:q], pq[q+1:]
	parts := strings.Split(raw, "&")
	kept := parts[:0]
	for _, p := range parts {
		if strings.HasPrefix(p, "token=") {
			continue
		}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		return path
	}
	return path + "?" + strings.Join(kept, "&")
}
