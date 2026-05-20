package server

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
				w.Header().Set("Access-Control-Allow-Headers", requested)
				w.Header().Add("Vary", "Access-Control-Request-Headers")
			}
			if requested := r.Header.Get("Access-Control-Request-Method"); requested != "" {
				w.Header().Set("Access-Control-Allow-Methods", strings.ToUpper(requested))
				w.Header().Add("Vary", "Access-Control-Request-Method")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withRequestLog logs method, path, status and duration for every request.
func (s *Server) withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		s.logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.status,
			"dur_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// withRecover converts panics in handlers to 500 errors and logs the trace.
func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic in handler",
					"panic", rec,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Hijack lets WebSocket upgrades succeed through this middleware. Without
// it, http.ResponseController on the wrapped writer can't reach the
// hijacker on the underlying connection.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := s.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("response writer does not support hijacking")
}

// Flush is forwarded so streaming endpoints can flush through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
