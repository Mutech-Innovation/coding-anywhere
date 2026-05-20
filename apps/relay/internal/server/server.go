package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
)

// Server is the relay HTTP/WS server. It intentionally exposes a small surface:
// New() builds it, Run() runs it until ctx is cancelled, and Close() releases
// resources.
type Server struct {
	cfg    Config
	logger *slog.Logger
	signer *auth.Signer
	store  *store.Store
	hub    *Hub
	owns   bool // true if we opened the store and should close it

	httpSrv *http.Server
}

// New constructs a Server from a validated config and a logger.
//
// It opens (or creates) the sqlite store under cfg.DataDir/relay.db. Use
// NewWithStore in tests when you want to inject a pre-built store.
func New(cfg Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	st, err := store.Open(context.Background(), filepath.Join(cfg.DataDir, "relay.db"))
	if err != nil {
		return nil, err
	}
	s, err := NewWithStore(cfg, logger, st)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	s.owns = true
	return s, nil
}

// NewWithStore is like New but uses the given store rather than opening one.
// The caller retains ownership of the store and must close it.
func NewWithStore(cfg Config, logger *slog.Logger, st *store.Store) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	signer, err := auth.NewSigner(cfg.JWTSecret, cfg.JWTIssuer, nil)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, logger: logger, signer: signer, store: st, hub: NewHub(logger)}
	s.httpSrv = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           s.routes(),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	return s, nil
}

// Close releases resources held by the server. Safe to call multiple times.
func (s *Server) Close() error {
	if s.owns && s.store != nil {
		err := s.store.Close()
		s.store = nil
		return err
	}
	return nil
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

// Store returns the underlying sqlite store (test-only access).
func (s *Server) Store() *store.Store { return s.store }
