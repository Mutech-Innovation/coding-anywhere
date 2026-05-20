package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// peerSendBuffer bounds the per-connection outbound queue. If a client falls
// behind for too long the relay drops the slow consumer; this prevents one
// stuck device from causing memory growth on the relay.
const peerSendBuffer = 64

// Conn is one authenticated WebSocket connection on the relay. The hub holds
// these, indexed by account, and broadcasts envelopes between them.
type Conn struct {
	id        string
	accountID string
	deviceID  string
	kind      auth.DeviceKind

	ws   *websocket.Conn
	send chan wire.Envelope

	// closed is set once the conn has been unregistered, preventing repeat
	// Hub.Unregister calls from interfering with each other.
	closed atomic.Bool

	// serverPending tracks responses the relay itself is waiting for
	// (e.g. injected from the HTTP proxy endpoint). When the read loop
	// sees a `res` envelope whose id is in this map, it delivers it to
	// the channel and skips peer fan-out.
	serverPendingMu sync.Mutex
	serverPending   map[string]chan wire.Envelope
}

// Account aggregates all connections for one account.
type Account struct {
	daemons map[string]*Conn // device_id → conn
	clients map[string]*Conn // device_id → conn
}

// Hub is the in-memory routing table. It is safe for concurrent use.
type Hub struct {
	logger *slog.Logger

	mu    sync.RWMutex
	accs  map[string]*Account
}

// NewHub returns an empty Hub.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{logger: logger, accs: make(map[string]*Account)}
}

// Register adds c to the routing table.
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	acc, ok := h.accs[c.accountID]
	if !ok {
		acc = &Account{
			daemons: make(map[string]*Conn),
			clients: make(map[string]*Conn),
		}
		h.accs[c.accountID] = acc
	}
	switch c.kind {
	case auth.DeviceDaemon:
		acc.daemons[c.deviceID] = c
	case auth.DeviceClient:
		acc.clients[c.deviceID] = c
	}
}

// Unregister removes c from the routing table. Safe to call multiple times.
func (h *Hub) Unregister(c *Conn) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	acc, ok := h.accs[c.accountID]
	if !ok {
		return
	}
	switch c.kind {
	case auth.DeviceDaemon:
		delete(acc.daemons, c.deviceID)
	case auth.DeviceClient:
		delete(acc.clients, c.deviceID)
	}
	if len(acc.daemons) == 0 && len(acc.clients) == 0 {
		delete(h.accs, c.accountID)
	}
}

// CountDevices returns (daemons, clients) currently registered for an account.
func (h *Hub) CountDevices(accountID string) (int, int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	acc, ok := h.accs[accountID]
	if !ok {
		return 0, 0
	}
	return len(acc.daemons), len(acc.clients)
}

// pickDaemon returns any one connected daemon for the given account.
func (h *Hub) pickDaemon(accountID string) *Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	acc, ok := h.accs[accountID]
	if !ok {
		return nil
	}
	for _, c := range acc.daemons {
		return c
	}
	return nil
}

// ServerRequest sends an envelope from the relay itself to a daemon and
// awaits the response. Used by HTTP shims (e.g. the ports proxy endpoint)
// that need to round-trip through the daemon synchronously. The envelope's
// `ID` must be unique. Returns ErrNoDaemon if no daemon is connected for
// the account, or context.DeadlineExceeded on timeout.
func (h *Hub) ServerRequest(ctx context.Context, accountID string, env wire.Envelope) (wire.Envelope, error) {
	conn := h.pickDaemon(accountID)
	if conn == nil {
		return wire.Envelope{}, ErrNoDaemon
	}
	ch := make(chan wire.Envelope, 1)
	conn.serverPendingMu.Lock()
	if conn.serverPending == nil {
		conn.serverPending = map[string]chan wire.Envelope{}
	}
	conn.serverPending[env.ID] = ch
	conn.serverPendingMu.Unlock()
	defer func() {
		conn.serverPendingMu.Lock()
		delete(conn.serverPending, env.ID)
		conn.serverPendingMu.Unlock()
	}()
	select {
	case conn.send <- env:
	case <-ctx.Done():
		return wire.Envelope{}, ctx.Err()
	}
	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
		return wire.Envelope{}, ctx.Err()
	}
}

// takeServerPending returns the chan registered for `id` (if any) and
// removes it. Called by wsReadLoop on every `res` envelope to redirect
// server-injected responses away from the broadcast path.
func (c *Conn) takeServerPending(id string) (chan wire.Envelope, bool) {
	c.serverPendingMu.Lock()
	defer c.serverPendingMu.Unlock()
	if c.serverPending == nil {
		return nil, false
	}
	ch, ok := c.serverPending[id]
	if ok {
		delete(c.serverPending, id)
	}
	return ch, ok
}

// ErrNoDaemon is returned by ServerRequest when no daemon is currently
// connected for the requested account.
var ErrNoDaemon = errors.New("no daemon connected for account")

// Broadcast forwards env to peers on the same account, with kind-aware
// fan-out: a daemon's envelope goes to every client; a client's envelope
// goes to every daemon. Senders never receive their own message.
func (h *Hub) Broadcast(from *Conn, env wire.Envelope) {
	targets := h.targetsFor(from)
	if len(targets) == 0 {
		return
	}
	for _, peer := range targets {
		select {
		case peer.send <- env:
		default:
			h.logger.Warn("dropping envelope to slow peer",
				"acc", peer.accountID, "dev", peer.deviceID, "kind", peer.kind)
			peer.markStuck()
		}
	}
}

// targetsFor returns peers that should receive a message originating from
// `from`. Snapshotting under RLock keeps the broadcast path lock-free.
func (h *Hub) targetsFor(from *Conn) []*Conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	acc, ok := h.accs[from.accountID]
	if !ok {
		return nil
	}
	var targets []*Conn
	switch from.kind {
	case auth.DeviceDaemon:
		for _, c := range acc.clients {
			targets = append(targets, c)
		}
	case auth.DeviceClient:
		for _, c := range acc.daemons {
			targets = append(targets, c)
		}
	}
	return targets
}

// markStuck closes the conn so the read/write loops exit and free resources.
// It's a hint, not a hard guarantee — the goroutines will tear down via the
// normal path on the next iteration.
func (c *Conn) markStuck() {
	_ = c.ws.Close(websocket.StatusPolicyViolation, "send buffer full")
}

// runWriter pumps env values from c.send to the WebSocket. It exits when the
// channel is closed or the context is cancelled.
func (c *Conn) runWriter(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env, ok := <-c.send:
			if !ok {
				return nil
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(writeCtx, c.ws, env)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}


