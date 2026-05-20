package ptymux

import (
	"errors"
	"sync"

	"github.com/oklog/ulid/v2"
)

// Manager owns the daemon's set of PTY sessions. Sessions are keyed by ULID
// strings issued at Open() time. The zero value is not usable; call
// NewManager().
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Open spawns a new PTY session and registers it.
func (m *Manager) Open(opts SessionOptions) (*Session, error) {
	id := ulid.Make().String()
	s, err := newSession(id, opts)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	// Auto-deregister when the session terminates so List() reflects reality.
	go func() {
		<-s.Done()
		m.mu.Lock()
		delete(m.sessions, s.ID)
		m.mu.Unlock()
	}()
	return s, nil
}

// Get returns a session by id, or ErrNotFound.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

// List returns metadata for every running session.
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	return out
}

// Close terminates a session by id. ErrNotFound if missing. Idempotent for
// already-closed sessions.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	delete(m.sessions, id)
	m.mu.Unlock()
	return s.Close()
}

// CloseAll terminates every session. Returns the first error encountered.
func (m *Manager) CloseAll() error {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	var firstErr error
	for _, s := range all {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ErrNotFound is returned by Get/Close when no session matches the id.
var ErrNotFound = errors.New("session not found")
