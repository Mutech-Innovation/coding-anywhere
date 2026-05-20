package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

// Server is the relay HTTP/WS server. It intentionally exposes a small surface:
// New() builds it, ListenAndServe() runs it until ctx is cancelled, and
// Shutdown() gracefully stops it.
type Server struct {
	cfg    Config
	logger *slog.Logger
	signer *auth.Signer

	httpSrv *http.Server
}

// New constructs a Server from a validated config and a logger.
func New(cfg Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	signer, err := auth.NewSigner(cfg.JWTSecret, cfg.JWTIssuer, nil)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{cfg: cfg, logger: logger, signer: signer}
	s.httpSrv = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           s.routes(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	return s, nil
}

// Run binds the listener, then serves until ctx is cancelled. It performs a
// graceful shutdown with a 10s timeout when ctx ends.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	s.logger.Info("relay listening", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() {
		err := s.httpSrv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// Signer returns the JWT signer the server uses (test-only access).
func (s *Server) Signer() *auth.Signer { return s.signer }
