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

// handleWSConnect upgrades the connection to WebSocket and runs a per-connection
// loop. Authentication is via Bearer JWT in the Authorization header (preferred),
// or `?token=` query param as a fallback (browsers can't set custom headers on
// the WebSocket handshake).
//
// P1.1 only echoes meta-channel ping/pong frames so we can prove the
// transport end-to-end. P1.3 wires the account routing layer.
func (s *Server) handleWSConnect(w http.ResponseWriter, r *http.Request) {
	claims, err := s.authenticateWS(r)
	if err != nil {
		s.logger.Warn("ws auth rejected", "err", err, "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Accept connections from any origin since the dev machine may be on
		// a different LAN than the client.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Warn("ws accept", "err", err)
		return
	}
	defer conn.Close(websocket.StatusInternalError, "shutdown")

	logger := s.logger.With("acc", claims.AccountID, "dev", claims.DeviceID, "kind", claims.Kind)
	logger.Info("ws connected")
	defer logger.Info("ws disconnected")

	if err := s.wsLoop(r.Context(), conn, claims, logger); err != nil &&
		!errors.Is(err, context.Canceled) &&
		websocket.CloseStatus(err) == -1 {
		logger.Warn("ws loop ended", "err", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

// wsLoop reads envelopes and echoes meta.ping → meta.pong. Other channels
// reply with a generic error envelope so clients see the relay is alive but
// the channel isn't routed yet.
func (s *Server) wsLoop(ctx context.Context, conn *websocket.Conn, claims *auth.Claims, logger *slog.Logger) error {
	for {
		var env wire.Envelope
		if err := wsjson.Read(ctx, conn, &env); err != nil {
			return err
		}
		if env.ID == "" {
			_ = wsjson.Write(ctx, conn, mustEnvelope(wire.MakeResponse("0", env.Channel, nil, &wire.EnvelopeError{
				Code:    "invalid_envelope",
				Message: "id required",
			})))
			continue
		}
		if !wire.ValidChannel(env.Channel) {
			_ = wsjson.Write(ctx, conn, mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
				Code:    "unknown_channel",
				Message: "unknown channel",
			})))
			continue
		}

		if env.Channel == wire.ChannelMeta {
			s.handleMeta(ctx, conn, env, claims, logger)
			continue
		}

		// All other channels are not yet routed — return a stable error so
		// clients can rely on the shape.
		_ = wsjson.Write(ctx, conn, mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
			Code:    "channel_not_routed",
			Message: "channel routing lands in P1.3",
		})))
	}
}

func (s *Server) handleMeta(ctx context.Context, conn *websocket.Conn, env wire.Envelope, _ *auth.Claims, logger *slog.Logger) {
	switch env.Method {
	case "ping":
		var p struct {
			Nonce string `json:"nonce"`
		}
		if len(env.Payload) > 0 {
			if err := json.Unmarshal(env.Payload, &p); err != nil {
				_ = wsjson.Write(ctx, conn, mustEnvelope(wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
					Code:    "bad_payload",
					Message: err.Error(),
				})))
				return
			}
		}
		_ = wsjson.Write(ctx, conn, mustEnvelope(wire.MakeResponse(env.ID, wire.ChannelMeta, map[string]any{
			"nonce":          p.Nonce,
			"server_time_ms": time.Now().UnixMilli(),
		}, nil)))
	default:
		logger.Debug("unhandled meta method", "method", env.Method)
		_ = wsjson.Write(ctx, conn, mustEnvelope(wire.MakeResponse(env.ID, wire.ChannelMeta, nil, &wire.EnvelopeError{
			Code:    "unknown_method",
			Message: "unknown meta method",
		})))
	}
}

func (s *Server) authenticateWS(r *http.Request) (*auth.Claims, error) {
	// Prefer Authorization: Bearer <token>; fall back to ?token=.
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
