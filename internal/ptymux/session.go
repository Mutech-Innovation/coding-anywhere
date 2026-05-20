// Package ptymux multiplexes PTY-backed processes for the daemon.
//
// Each Session owns one child process (a shell, by default) running behind a
// PTY. The session fans the PTY's output out to multiple subscribers, keeps
// a fixed-size ring buffer for scrollback, and accepts input + resize
// commands from any subscriber.
//
// The package is OS-portable through `github.com/creack/pty`. Windows is
// out-of-scope for the MVP.
package ptymux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/creack/pty"
)

// SessionOptions controls how a new PTY-backed session is spawned.
type SessionOptions struct {
	// Command is argv. Defaults to the user's $SHELL, or "/bin/sh" if unset.
	Command []string
	// Cwd is the working directory of the child. Defaults to $HOME (or os.Getwd).
	Cwd string
	// Env is the environment passed to the child. nil means inherit os.Environ().
	Env []string
	// Cols / Rows are the initial PTY size. Defaults to 80x24.
	Cols, Rows uint16
	// ScrollbackBytes caps the ring buffer per session. Defaults to 1MiB.
	ScrollbackBytes int
}

// SessionInfo is the public-facing summary of a session, returned via List().
type SessionInfo struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	Cols      uint16    `json:"cols"`
	Rows      uint16    `json:"rows"`
	StartedAt time.Time `json:"started_at"`
	Command   []string  `json:"command"`
	Cwd       string    `json:"cwd"`
}

// Session is a running PTY-backed process. Construct via Manager.Open.
type Session struct {
	ID        string
	StartedAt time.Time

	mu          sync.Mutex
	pty         *os.File
	cmd         *exec.Cmd
	command     []string
	cwd         string
	cols, rows  uint16
	closed      atomic.Bool
	subscribers map[uint64]chan []byte
	nextSubID   uint64
	ring        *ringBuffer

	doneCh chan struct{}
	exitCh chan ExitEvent
}

// ExitEvent is delivered via Wait() once the session terminates.
type ExitEvent struct {
	Code   int
	Signal int
	Err    error
}

// newSession spawns a child process attached to a PTY.
func newSession(id string, opts SessionOptions) (*Session, error) {
	cmd := commandFromOpts(opts)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	cols := opts.Cols
	rows := opts.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	ws := &pty.Winsize{Cols: cols, Rows: rows}
	f, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}

	s := &Session{
		ID:          id,
		StartedAt:   time.Now().UTC(),
		pty:         f,
		cmd:         cmd,
		command:     cmd.Args,
		cwd:         cmd.Dir,
		cols:        cols,
		rows:        rows,
		subscribers: make(map[uint64]chan []byte),
		ring:        newRingBuffer(orDefault(opts.ScrollbackBytes, 1<<20)),
		doneCh:      make(chan struct{}),
		exitCh:      make(chan ExitEvent, 1),
	}

	go s.readLoop()
	go s.waitLoop()
	return s, nil
}

// Info returns a snapshot of identifying metadata for this session.
func (s *Session) Info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid := 0
	if s.cmd != nil && s.cmd.Process != nil {
		pid = s.cmd.Process.Pid
	}
	return SessionInfo{
		ID: s.ID, PID: pid,
		Cols: s.cols, Rows: s.rows,
		StartedAt: s.StartedAt,
		Command:   append([]string{}, s.command...),
		Cwd:       s.cwd,
	}
}

// Write injects user input into the PTY. Bytes go straight to the child's stdin.
func (s *Session) Write(b []byte) (int, error) {
	if s.closed.Load() {
		return 0, ErrSessionClosed
	}
	return s.pty.Write(b)
}

// Resize tells the kernel the new PTY size. Most TUIs respond to SIGWINCH.
func (s *Session) Resize(cols, rows uint16) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if cols == 0 || rows == 0 {
		return errors.New("cols and rows must be > 0")
	}
	if err := pty.Setsize(s.pty, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return err
	}
	s.mu.Lock()
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	return nil
}

// Subscribe returns a channel of output chunks plus a function to detach.
//
// The returned channel is buffered; if the consumer falls too far behind,
// the session DROPS chunks for that subscriber rather than block. The drop
// is silent — clients that care should track lag via their own protocol
// (e.g. send periodic byte counts).
//
// `bufSize` controls the per-subscriber buffer. Pass 0 for the default (256).
//
// Snapshot() of current scrollback is returned alongside so clients can
// render history before the first live chunk lands.
func (s *Session) Subscribe(bufSize int) (id uint64, snapshot []byte, ch <-chan []byte, detach func()) {
	if bufSize <= 0 {
		bufSize = 256
	}
	c := make(chan []byte, bufSize)
	s.mu.Lock()
	s.nextSubID++
	id = s.nextSubID
	s.subscribers[id] = c
	s.mu.Unlock()
	snapshot = s.ring.Snapshot()
	detach = func() { s.unsubscribe(id) }
	ch = c
	return
}

func (s *Session) unsubscribe(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.subscribers[id]; ok {
		close(c)
		delete(s.subscribers, id)
	}
}

// Close terminates the child process and tears down the PTY. Safe to call
// multiple times.
func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.pty.Close()
	return nil
}

// Wait returns the child's exit info (blocking until then or ctx is cancelled).
func (s *Session) Wait(ctx context.Context) (ExitEvent, error) {
	select {
	case e := <-s.exitCh:
		return e, nil
	case <-ctx.Done():
		return ExitEvent{}, ctx.Err()
	}
}

// Done returns a channel that's closed when the session terminates.
func (s *Session) Done() <-chan struct{} { return s.doneCh }

func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.ring.Write(chunk)
			s.fanOut(chunk)
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) waitLoop() {
	err := s.cmd.Wait()
	exit := ExitEvent{}
	if err != nil {
		exit.Err = err
	}
	if ws, ok := s.cmd.ProcessState.Sys().(interface {
		ExitStatus() int
	}); ok {
		exit.Code = ws.ExitStatus()
	}
	// Mark closed and broadcast.
	s.closed.Store(true)
	_ = s.pty.Close()
	close(s.doneCh)
	select {
	case s.exitCh <- exit:
	default:
	}
	// Close all subscriber channels to wake them.
	s.mu.Lock()
	for id, c := range s.subscribers {
		close(c)
		delete(s.subscribers, id)
	}
	s.mu.Unlock()
}

func (s *Session) fanOut(chunk []byte) {
	s.mu.Lock()
	subs := make([]chan []byte, 0, len(s.subscribers))
	for _, c := range s.subscribers {
		subs = append(subs, c)
	}
	s.mu.Unlock()
	for _, c := range subs {
		select {
		case c <- chunk:
		default:
			// Drop; the slow consumer will fall behind.
		}
	}
}

func commandFromOpts(opts SessionOptions) *exec.Cmd {
	cmd := opts.Command
	if len(cmd) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd = []string{shell, "-l"}
	}
	c := exec.Command(cmd[0], cmd[1:]...)
	if opts.Env == nil {
		c.Env = os.Environ()
	}
	return c
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// ErrSessionClosed is returned by Write/Resize after Close() or after the
// child process has exited.
var ErrSessionClosed = errors.New("session closed")

// Ensure Session satisfies the io.Writer interface (handy for input shims).
var _ io.Writer = (*Session)(nil)
