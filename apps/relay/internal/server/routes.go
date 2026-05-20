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
	mux.HandleFunc("GET /v1/ws/connect", s.handleWSConnect)

	// P1.2 will add: POST /v1/pair/init, POST /v1/pair/claim, GET /v1/pair/status
	return s.withRecover(s.withRequestLog(mux))
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
