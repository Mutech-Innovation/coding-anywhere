package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// routes wires the HTTP mux. Routes are intentionally hand-written so the
// relay has no router dependency. Method+path patterns require Go 1.22+.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/version", s.handleVersion)

	// Pairing
	mux.HandleFunc("POST /v1/pair/init", s.handlePairInit)
	mux.HandleFunc("GET /v1/pair/status", s.handlePairStatus)
	mux.HandleFunc("POST /v1/pair/claim", s.handlePairClaim)

	// Auth
	mux.HandleFunc("POST /v1/auth/refresh", s.handleAuthRefresh)
	mux.HandleFunc("POST /v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /v1/auth/key/rotate", s.handleAuthKeyRotate)
	mux.HandleFunc("DELETE /v1/auth/key", s.handleAuthKeyRevoke)

	// Push notifications
	mux.HandleFunc("GET /v1/push/vapid_public_key", s.handleVAPIDPublicKey)
	mux.HandleFunc("POST /v1/push/subscribe", s.handlePushSubscribe)
	mux.HandleFunc("POST /v1/push/unsubscribe", s.handlePushUnsubscribe)
	mux.HandleFunc("POST /v1/push/test", s.handlePushTest)

	// Guest "ask"-mode share links
	mux.HandleFunc("POST /v1/share/ask", s.handleShareAsk)
	mux.HandleFunc("DELETE /v1/share/{id}", s.handleShareRevoke)

	// WebSocket
	mux.HandleFunc("GET /v1/ws/connect", s.handleWSConnect)

	// HTTP proxy to the daemon's localhost ports.
	mux.HandleFunc("/v1/proxy/", s.handleProxy)

	return s.withCORS(s.withRecover(s.withRequestLog(mux)))
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "time": time.Now().UTC()})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"protocol_version": 1,
		"issuer":           s.cfg.JWTIssuer,
	})
}
