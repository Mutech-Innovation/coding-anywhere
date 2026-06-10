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

	// guest holds the per-connection state for a read-only "ask"-mode share.
	// nil for daemon/client connections.
	guest *guestState
}

// guestState carries the isolation + rate-limit bookkeeping for one guest
// (share-link) connection. All access is guarded by mu.
type guestState struct {
	mu sync.Mutex
	// owned is the set of chat session ids this guest created and is allowed
	// to send to / receive events for.
	owned map[string]bool
	// pending maps an in-flight request id (sent by the guest) to its chat
	// method, so the relay can route the daemon's response back to only this
	// guest and learn the session id from a create_session response.
	pending map[string]string
	// limits, fixed at issuance.
	promptLimit int
	maxSessions int
	// cwd is the owner-chosen working directory injected into the guest's
	// create_session. Immutable after construction. Empty means "don't
	// inject" — the daemon then applies its own cwd policy.
	cwd string
	// model is the owner-chosen model injected into the guest's
	// create_session. Immutable after construction. Empty means "don't
	// inject" — the guest session uses the agent's default model.
	model string
	// agentID is the owner-chosen chat agent injected into the guest's
	// create_session. Immutable after construction. Empty means "don't
	// inject" — the daemon then applies its default agent (devin).
	agentID string
	// counters.
	promptsUsed     int
	sessionsCreated int
}

// Account aggregates all connections for one account.
type Account struct {
	daemons map[string]*Conn // device_id → conn
	// clients is keyed by the unique connection id, NOT the device id: every
	// tab of one paired browser shares a single device id (localStorage), so
	// keying by device id would force a single tab and make concurrent tabs
	// evict each other in a reconnect storm. Multiple tabs must coexist.
	clients map[string]*Conn // conn id → conn
	// guests is keyed by the unique connection id, NOT the device id: all
	// viewers of one share link share a single device id (the share id), so
	// keying by device id would force a single guest and make concurrent
	// viewers evict each other in a reconnect storm. Multiple guests per
	// share must coexist.
	guests map[string]*Conn // conn id → conn
}

func (c *Conn) isGuest() bool { return c.guest != nil }

// Hub is the in-memory routing table. It is safe for concurrent use.
type Hub struct {
	logger *slog.Logger

	mu   sync.RWMutex
	accs map[string]*Account
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
			guests:  make(map[string]*Conn),
		}
		h.accs[c.accountID] = acc
	}
	switch c.kind {
	case auth.DeviceDaemon:
		if old := acc.daemons[c.deviceID]; old != nil && old != c {
			go func() { _ = old.ws.Close(websocket.StatusNormalClosure, "replaced") }()
		}
		acc.daemons[c.deviceID] = c
	case auth.DeviceClient:
		// No eviction: keyed by connection id so multiple tabs of the same
		// paired browser coexist instead of evicting each other. Stale
		// sockets reap themselves via their own read/write loops.
		acc.clients[c.id] = c
	case auth.DeviceGuest:
		// No eviction: keyed by connection id so multiple viewers of the
		// same share link coexist instead of evicting each other.
		acc.guests[c.id] = c
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
		if acc.daemons[c.deviceID] == c {
			delete(acc.daemons, c.deviceID)
		}
	case auth.DeviceClient:
		if acc.clients[c.id] == c {
			delete(acc.clients, c.id)
		}
	case auth.DeviceGuest:
		if acc.guests[c.id] == c {
			delete(acc.guests, c.id)
		}
	}
	if len(acc.daemons) == 0 && len(acc.clients) == 0 && len(acc.guests) == 0 {
		delete(h.accs, c.accountID)
	}
}

// CloseGuest drops every live guest connection for the given account whose
// device id (share id) matches. Used when a share is revoked. Guests are
// keyed by connection id, so a single share may have several live viewers —
// all of them must be dropped.
func (h *Hub) CloseGuest(accountID, shareID string) {
	h.mu.RLock()
	acc, ok := h.accs[accountID]
	var conns []*Conn
	if ok {
		for _, c := range acc.guests {
			if c.deviceID == shareID {
				conns = append(conns, c)
			}
		}
	}
	h.mu.RUnlock()
	for _, conn := range conns {
		_ = conn.ws.Close(websocket.StatusPolicyViolation, "share revoked")
	}
}

// CountDevices returns (daemons, clients) currently registered for an account.
// Clients are counted as distinct devices, not connections, so several tabs
// of one paired browser still count once.
func (h *Hub) CountDevices(accountID string) (int, int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	acc, ok := h.accs[accountID]
	if !ok {
		return 0, 0
	}
	clientDevs := make(map[string]struct{}, len(acc.clients))
	for _, c := range acc.clients {
		clientDevs[c.deviceID] = struct{}{}
	}
	return len(acc.daemons), len(clientDevs)
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
// fan-out: a daemon's envelope goes to every client; a client's (or guest's)
// envelope goes to every daemon. Senders never receive their own message.
//
// Guest connections are session-isolated: a daemon's envelope is delivered to
// a guest only if it concerns a chat session the guest owns (or is the
// response to one of the guest's own requests). See deliverToGuest.
func (h *Hub) Broadcast(from *Conn, env wire.Envelope) {
	targets := h.targetsFor(from)
	if len(targets) == 0 {
		if (from.kind == auth.DeviceClient || from.kind == auth.DeviceGuest) &&
			env.Type == wire.TypeRequest {
			from.deliver(context.Background(), mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
				Code:    "no_daemon",
				Message: "no daemon connected for this account; sign out and pair with the active daemon",
			})))
		}
		return
	}
	for _, peer := range targets {
		if peer.isGuest() {
			h.deliverToGuest(peer, env)
			continue
		}
		select {
		case peer.send <- env:
		default:
			h.logger.Warn("dropping envelope to slow peer",
				"acc", peer.accountID, "dev", peer.deviceID, "kind", peer.kind)
			peer.markStuck()
		}
	}
}

// deliverToGuest forwards a daemon envelope to a guest only when the guest is
// allowed to see it. It also learns the guest's session ownership from a
// create_session response so subsequent events for that session pass the
// isolation filter.
func (h *Hub) deliverToGuest(g *Conn, env wire.Envelope) {
	if !guestMayReceive(g, env) {
		return
	}
	select {
	case g.send <- env:
	default:
		h.logger.Warn("dropping envelope to slow guest", "acc", g.accountID, "dev", g.deviceID)
		g.markStuck()
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
		for _, c := range acc.guests {
			targets = append(targets, c)
		}
	case auth.DeviceClient, auth.DeviceGuest:
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
