package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
//   1. Validate the JWT (account, device, kind).
//   2. Accept the WebSocket.
//   3. Register the conn with the hub.
//   4. Spawn a writer goroutine pumping conn.send → ws.
//   5. Read loop:
//        - meta channel  → handle locally (ping/pong).
//        - other channels → fan-out to peer devices via Hub.Broadcast.
//   6. On exit: cancel writer, unregister, close ws.
func (s *Server) handleWSConnect(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticateWS(r)
	if err != nil {
		s.logger.Warn("ws auth rejected", "err", err, "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
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
	}
	s.hub.Register(conn)
	logger := s.logger.With("acc", conn.accountID, "dev", conn.deviceID, "kind", conn.kind, "conn", conn.id)
	logger.Info("ws connected")
	if s.store != nil {
		_ = s.store.TouchDevice(r.Context(), conn.deviceID)
	}

	defer func() {
		s.hub.Unregister(conn)
		_ = ws.Close(websocket.StatusNormalClosure, "")
		logger.Info("ws disconnected")
	}()

	// Writer pump.
	writerCtx, cancelWriter := context.WithCancel(r.Context())
	defer cancelWriter()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		if err := conn.runWriter(writerCtx); err != nil &&
			!errors.Is(err, context.Canceled) {
			logger.Debug("writer ended", "err", err)
		}
	}()

	if err := s.wsReadLoop(r.Context(), conn, logger); err != nil &&
		!errors.Is(err, context.Canceled) &&
		websocket.CloseStatus(err) == -1 {
		logger.Warn("read loop ended", "err", err)
	}
	cancelWriter()
	<-writerDone
}

// wsReadLoop is the per-connection read pump.
func (s *Server) wsReadLoop(ctx context.Context, conn *Conn, logger *slog.Logger) error {
	for {
		var env wire.Envelope
		if err := wsjson.Read(ctx, conn.ws, &env); err != nil {
			return err
		}
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
