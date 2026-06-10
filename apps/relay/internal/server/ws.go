package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/auth"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// handleWSConnect upgrades the connection to WebSocket and routes its
// envelopes through the relay hub.
//
// Authentication: Bearer JWT in the Authorization header (preferred) or
// `?token=` query param fallback (browsers can't set custom headers on WS).
//
// Per-connection lifecycle:
//  1. Validate the JWT (account, device, kind).
//  2. Accept the WebSocket.
//  3. Register the conn with the hub.
//  4. Spawn a writer goroutine pumping conn.send → ws.
//  5. Read loop:
//     - meta channel  → handle locally (ping/pong).
//     - other channels → fan-out to peer devices via Hub.Broadcast.
//  6. On exit: cancel writer, unregister, close ws.
func (s *Server) handleWSConnect(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticateWS(r)
	if err != nil {
		s.logger.Warn("ws auth rejected", "err", err, "remote", r.RemoteAddr)
		// Browsers can't read the HTTP status of a failed WS handshake, so a
		// plain 401 leaves the guest share page reconnect-looping with no
		// clue. Guest tokens can't be refreshed either — when the rejected
		// token *claims* to be a guest, accept the socket and close it with
		// an explicit reason the page can display.
		if guestKindUnverified(r) {
			s.rejectWS(w, r, "token expired or invalid")
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Guests carry per-link limits and revocation state in the store; resolve
	// them before accepting so a revoked/expired share can't open a socket.
	var guest *guestState
	if claims.Kind == auth.DeviceGuest {
		gs, err := s.resolveGuest(r.Context(), claims)
		if err != nil {
			s.logger.Warn("guest connect rejected", "err", err, "remote", r.RemoteAddr)
			s.rejectWS(w, r, guestRejectReason(err))
			return
		}
		guest = gs
	}

	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Accept connections from any origin since the dev machine may be on
		// a different LAN than the client.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Warn("ws accept", "err", err)
		return
	}
	ws.SetReadLimit(1 << 20) // 1 MiB

	conn := &Conn{
		id:        newULID(),
		accountID: claims.AccountID,
		deviceID:  claims.DeviceID,
		kind:      claims.Kind,
		ws:        ws,
		send:      make(chan wire.Envelope, peerSendBuffer),
		guest:     guest,
	}
	s.hub.Register(conn)
	logger := s.logger.With("acc", conn.accountID, "dev", conn.deviceID, "kind", conn.kind, "conn", conn.id)
	logger.Info("ws connected")
	if s.store != nil && !conn.isGuest() {
		_ = s.store.TouchDevice(r.Context(), conn.deviceID)
	}

	defer func() {
		s.hub.Unregister(conn)
		_ = ws.Close(websocket.StatusNormalClosure, "")
		logger.Info("ws disconnected")
	}()

	// Connection-scoped context. Guests get a hard deadline at token expiry so
	// an already-open socket can't outlive the share.
	connCtx := r.Context()
	if conn.isGuest() && claims.ExpiresAt != nil {
		var cancelDeadline context.CancelFunc
		connCtx, cancelDeadline = context.WithDeadline(connCtx, claims.ExpiresAt.Time)
		defer cancelDeadline()
	}

	// Writer pump.
	writerCtx, cancelWriter := context.WithCancel(connCtx)
	defer cancelWriter()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		if err := conn.runWriter(writerCtx); err != nil &&
			!errors.Is(err, context.Canceled) {
			logger.Debug("writer ended", "err", err)
		}
	}()

	if err := s.wsReadLoop(connCtx, conn, logger); err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) &&
		websocket.CloseStatus(err) == -1 {
		logger.Warn("read loop ended", "err", err)
	}
	cancelWriter()
	<-writerDone
}

// Guest share rejection causes, mapped onto the WS close reason so the share
// page can show why instead of reconnect-looping forever.
var (
	errGuestShareUnknown = errors.New("unknown share")
	errGuestShareRevoked = errors.New("guest share revoked")
	errGuestShareExpired = errors.New("guest share expired")
)

// guestCloseStatus is the application close code used when a guest connection
// is rejected. Mirrored in the web GuestApp (4401 "unauthorized").
const guestCloseStatus = websocket.StatusCode(4401)

// resolveGuest validates a guest's share record (existence, revocation,
// expiry) and returns freshly-seeded per-connection guest state with the
// share's limits.
func (s *Server) resolveGuest(ctx context.Context, claims *auth.Claims) (*guestState, error) {
	if s.store == nil {
		return nil, errors.New("store unavailable")
	}
	share, err := s.store.GetGuestShare(ctx, claims.DeviceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errGuestShareUnknown
	}
	if err != nil {
		return nil, fmt.Errorf("guest share lookup: %w", err)
	}
	if share.AccountID != claims.AccountID {
		return nil, errors.New("guest share account mismatch")
	}
	if share.Revoked {
		return nil, errGuestShareRevoked
	}
	if time.Now().UTC().After(share.ExpiresAt) {
		return nil, errGuestShareExpired
	}
	return newGuestState(share.PromptLimit, share.MaxSessions, share.Cwd, share.Model, share.AgentID), nil
}

// guestRejectReason maps a resolveGuest error onto the short, stable close
// reason shipped to the guest page (close reasons are capped at 123 bytes).
func guestRejectReason(err error) string {
	switch {
	case errors.Is(err, errGuestShareExpired):
		return "share expired"
	case errors.Is(err, errGuestShareRevoked):
		return "share revoked"
	case errors.Is(err, errGuestShareUnknown):
		return "unknown share"
	default:
		return "unauthorized"
	}
}

// rejectWS upgrades the connection only to immediately close it with an
// application close code + reason. Browsers can't see the HTTP status of a
// failed WS handshake, but they DO receive close frames — this is the only
// channel we have to tell an unauthenticated guest page what went wrong.
func (s *Server) rejectWS(w http.ResponseWriter, r *http.Request, reason string) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	_ = ws.Close(guestCloseStatus, reason)
}

// guestKindUnverified reports whether the request's bearer token *claims* to
// be a guest token — WITHOUT verifying its signature or expiry. Used only to
// decide whether an auth failure should be reported over a WS close frame
// (guests) or a plain 401 (paired clients/daemons, which auto-refresh).
func guestKindUnverified(r *http.Request) bool {
	raw := r.URL.Query().Get("token")
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimPrefix(h, "Bearer ")
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var c struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return false
	}
	return c.Kind == string(auth.DeviceGuest)
}

// wsReadLoop is the per-connection read pump.
func (s *Server) wsReadLoop(ctx context.Context, conn *Conn, logger *slog.Logger) error {
	for {
		var env wire.Envelope
		if err := wsjson.Read(ctx, conn.ws, &env); err != nil {
			return err
		}
		// Origin is a relay-set trust marker; never honour one a peer set.
		// guestGate re-stamps it for allowed guest traffic below.
		env.Origin = ""
		if env.ID == "" {
			conn.deliver(ctx, mustEnvelope(wire.MakeResponse("0", env.Channel, nil, &wire.EnvelopeError{
				Code:    "invalid_envelope",
				Message: "id required",
			})))
			continue
		}
		if !wire.ValidChannel(env.Channel) {
			conn.deliver(ctx, mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
				Code:    "unknown_channel",
				Message: "unknown channel",
			})))
			continue
		}

		if env.Channel == wire.ChannelMeta {
			s.handleMeta(ctx, conn, env, logger)
			continue
		}

		// Server-injected requests (e.g. from the ports proxy endpoint)
		// register a waiter for their response id. Deliver and short-circuit.
		if env.Type == wire.TypeResponse {
			if ch, ok := conn.takeServerPending(env.ID); ok {
				ch <- env
				continue
			}
		}

		// Notifications from the daemon are also dispatched via Web Push so
		// offline / backgrounded devices still see them.
		if conn.kind == auth.DeviceDaemon &&
			env.Type == wire.TypeEvent &&
			env.Channel == wire.ChannelNotifications {
			go s.dispatchEnvelopePush(ctx, conn.accountID, env, logger)
		}

		// Guests are restricted to a safe chat subset, session-isolated, and
		// rate-limited. guestGate rejects disallowed traffic locally and
		// stamps Origin=guest on what it forwards.
		if conn.isGuest() && !s.guestGate(ctx, conn, &env) {
			continue
		}

		// Fan out to peers. Senders never see their own message.
		s.hub.Broadcast(conn, env)
	}
}

// handleMeta replies to ping locally so connectivity checks don't depend on
// having a peer connected.
func (s *Server) handleMeta(ctx context.Context, conn *Conn, env wire.Envelope, logger *slog.Logger) {
	switch env.Method {
	case "ping":
		var p struct {
			Nonce string `json:"nonce"`
		}
		if len(env.Payload) > 0 {
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				conn.deliver(ctx, mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
					Code:    "bad_payload",
					Message: err.Error(),
				})))
				return
			}
		}
		conn.deliver(ctx, mustEnvelope(wire.MakeResponse(env.ID, wire.ChannelMeta, map[string]any{
			"nonce":          p.Nonce,
			"server_time_ms": time.Now().UnixMilli(),
		}, nil)))
	default:
		logger.Debug("unhandled meta method", "method", env.Method)
		conn.deliver(ctx, mustEnvelope(wire.MakeResponse(env.ID, wire.ChannelMeta, nil, &wire.EnvelopeError{
			Code:    "unknown_method",
			Message: "unknown meta method",
		})))
	}
}

// deliver enqueues env on the conn's outbound channel. Drops the message if
// the buffer is full to avoid blocking the read loop.
func (c *Conn) deliver(ctx context.Context, env wire.Envelope) {
	select {
	case c.send <- env:
	case <-ctx.Done():
	default:
		// Buffer full; mark this conn as stuck.
		c.markStuck()
	}
}

func (s *Server) authenticateWS(r *http.Request) (*auth.Claims, error) {
	var raw string
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimPrefix(h, "Bearer ")
	} else if t := r.URL.Query().Get("token"); t != "" {
		raw = t
	}
	if raw == "" {
		return nil, errors.New("missing token")
	}
	return s.signer.Verify(raw)
}

func mustEnvelope(env wire.Envelope, err error) wire.Envelope {
	if err != nil {
		// Only happens if json.Marshal of payload fails, which we control.
		// Fall back to an envelope describing the encoding failure.
		return wire.Envelope{ID: env.ID, Type: wire.TypeResponse, Channel: env.Channel, Error: &wire.EnvelopeError{
			Code: "encode_error", Message: err.Error(),
		}}
	}
	return env
}
