package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/Mutech-Innovation/coding-anywhere/apps/relay/internal/store"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/oklog/ulid/v2"
)

// vapidPublicKeyResponse is what GET /v1/push/vapid_public_key returns.
type vapidPublicKeyResponse struct {
	PublicKey string `json:"public_key"`
}

// pushSubscribeRequest matches the browser's PushSubscription.toJSON() shape.
type pushSubscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256DH string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	UserAgent string `json:"user_agent,omitempty"`
}

type pushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

type pushTestRequest struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// ensureVAPIDKey loads the relay's VAPID keypair, creating it on first run.
func (s *Server) ensureVAPIDKey(ctx context.Context) (store.VAPIDKey, error) {
	if s.store == nil {
		return store.VAPIDKey{}, errors.New("store unavailable")
	}
	k, err := s.store.LatestVAPIDKey(ctx)
	if err == nil {
		return k, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.VAPIDKey{}, err
	}
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return store.VAPIDKey{}, fmt.Errorf("generate vapid: %w", err)
	}
	k = store.VAPIDKey{
		ID:         ulid.Make().String(),
		PublicKey:  pub,
		PrivateKey: priv,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.PutVAPIDKey(ctx, k); err != nil {
		return store.VAPIDKey{}, err
	}
	return k, nil
}

func (s *Server) handleVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	k, err := s.ensureVAPIDKey(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vapid_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, vapidPublicKeyResponse{PublicKey: k.PublicKey})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var req pushSubscribeRequest
	if err := decodeJSON(r.Body, &req, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Endpoint == "" || req.Keys.P256DH == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "endpoint and keys required")
		return
	}
	sub := store.PushSubscription{
		ID:         ulid.Make().String(),
		AccountID:  claims.AccountID,
		DeviceID:   claims.DeviceID,
		Endpoint:   req.Endpoint,
		P256DHKey:  req.Keys.P256DH,
		AuthKey:    req.Keys.Auth,
		UserAgent:  req.UserAgent,
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.UpsertPushSubscription(r.Context(), sub); err != nil {
		s.logger.Error("UpsertPushSubscription", "err", err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var req pushUnsubscribeRequest
	if err := decodeJSON(r.Body, &req, 2048); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.store.DeletePushSubscription(r.Context(), claims.AccountID, req.Endpoint); err != nil {
		s.logger.Error("DeletePushSubscription", "err", err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	claims, err := s.authenticateBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	var req pushTestRequest
	_ = decodeJSON(r.Body, &req, 1024)
	if req.Title == "" {
		req.Title = "Coding Anywhere"
	}
	if req.Body == "" {
		req.Body = "Test notification"
	}
	n := pushNotificationPayload{
		Title:      req.Title,
		Body:       req.Body,
		Tag:        "test",
		ReceivedAt: time.Now().UnixMilli(),
	}
	delivered, failures := s.dispatchAccountPush(r.Context(), claims.AccountID, n)
	writeJSON(w, http.StatusOK, map[string]any{
		"delivered": delivered,
		"failures":  failures,
	})
}

// pushNotificationPayload is the JSON shape we send through Web Push to the
// service worker. Keep it small (HTTP-encoded payload is bounded by the
// push service to ~4 KiB).
type pushNotificationPayload struct {
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
	Tag        string `json:"tag,omitempty"`
	URL        string `json:"url,omitempty"`
	Icon       string `json:"icon,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	ReceivedAt int64  `json:"received_at"`
}

// dispatchAccountPush sends a notification to every active subscription for
// the account. Returns counts so the caller can observe delivery.
func (s *Server) dispatchAccountPush(ctx context.Context, accountID string, n pushNotificationPayload) (delivered, failures int) {
	if s.store == nil {
		return 0, 0
	}
	subs, err := s.store.ListPushSubscriptionsForAccount(ctx, accountID)
	if err != nil {
		s.logger.Error("ListPushSubscriptionsForAccount", "err", err)
		return 0, 0
	}
	if len(subs) == 0 {
		return 0, 0
	}
	k, err := s.ensureVAPIDKey(ctx)
	if err != nil {
		s.logger.Error("ensureVAPIDKey", "err", err)
		return 0, len(subs)
	}
	body, err := json.Marshal(n)
	if err != nil {
		return 0, len(subs)
	}
	pushOpts := &webpush.Options{
		TTL:             60,
		VAPIDPublicKey:  k.PublicKey,
		VAPIDPrivateKey: k.PrivateKey,
		Subscriber:      "mailto:relay@coding-anywhere.local",
	}
	for _, sub := range subs {
		ws := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys:     webpush.Keys{P256dh: sub.P256DHKey, Auth: sub.AuthKey},
		}
		res, err := webpush.SendNotificationWithContext(ctx, body, ws, pushOpts)
		if err != nil {
			s.logger.Warn("push send failed", "endpoint", shortEndpoint(sub.Endpoint), "err", err)
			failures++
			continue
		}
		// 410 Gone / 404 → endpoint is dead, drop it.
		if res.StatusCode == http.StatusGone || res.StatusCode == http.StatusNotFound {
			_ = s.store.DeletePushSubscription(ctx, accountID, sub.Endpoint)
			failures++
			_ = res.Body.Close()
			continue
		}
		if res.StatusCode >= 400 {
			s.logger.Warn("push 4xx/5xx",
				"endpoint", shortEndpoint(sub.Endpoint),
				"status", res.StatusCode)
			failures++
		} else {
			delivered++
		}
		_ = res.Body.Close()
	}
	return delivered, failures
}

// dispatchEnvelopePush is called from wsReadLoop when a daemon sends a
// notifications.send event. It decodes the envelope payload and fans out to
// the account's subscriptions.
func (s *Server) dispatchEnvelopePush(ctx context.Context, accountID string, env wire.Envelope, logger *slog.Logger) {
	if env.Method != wire.NotificationsMethodSend {
		return
	}
	var p wire.NotificationsSendEvent
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		logger.Warn("notifications.send parse", "err", err)
		return
	}
	if p.Title == "" {
		return
	}
	n := pushNotificationPayload{
		Title:      p.Title,
		Body:       p.Body,
		Tag:        p.Tag,
		URL:        p.URL,
		Icon:       p.Icon,
		SessionID:  p.SessionID,
		ReceivedAt: time.Now().UnixMilli(),
	}
	delivered, failures := s.dispatchAccountPush(ctx, accountID, n)
	logger.Debug("dispatch push",
		"acc", accountID, "title", p.Title,
		"delivered", delivered, "failures", failures)
}

func shortEndpoint(s string) string {
	if len(s) > 64 {
		return s[:32] + "…" + s[len(s)-16:]
	}
	return s
}
