// Package relayclient is the daemon-side WebSocket client used to maintain a
// long-lived connection to the relay.
//
// It handles:
//   - Bearer-authenticated dial via Authorization header
//   - Automatic reconnect with capped exponential backoff
//   - Outbound + inbound envelope queues with a per-channel handler registry
//   - Periodic meta.ping to keep NAT/keep-alive timers fresh
//
// The client is opinionated: it owns the WebSocket and exposes channels for
// outbound writes and per-channel handlers for inbound traffic. Application
// code (PTY, files, agents, etc.) registers handlers and emits events; it
// never touches the underlying *websocket.Conn directly.
package relayclient

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Config holds the inputs needed to dial the relay.
type Config struct {
	// RelayURL is the base relay URL, e.g. "wss://relay.example.com" or
	// "ws://localhost:8443". Trailing /v1/ws/connect is appended by the client.
	RelayURL string
	// AccessToken is the JWT issued at pair time or refresh.
	AccessToken string
	// PingInterval is how often to send meta.ping. Zero = 30s.
	PingInterval time.Duration
	// HandshakeTimeout bounds the WebSocket Dial step.
	HandshakeTimeout time.Duration
	// MaxBackoff caps the reconnect delay. Zero = 30s.
	MaxBackoff time.Duration
	// BackoffResetAfter resets the reconnect backoff once a session has
	// stayed up at least this long, so a drop after hours of healthy uptime
	// reconnects quickly instead of waiting out an accumulated delay.
	// Short-lived sessions (dial failures, instant evictions) keep growing
	// the backoff as before. Zero = 1m.
	BackoffResetAfter time.Duration
}

func (c *Config) defaults() {
	if c.PingInterval == 0 {
		c.PingInterval = 30 * time.Second
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.BackoffResetAfter == 0 {
		c.BackoffResetAfter = time.Minute
	}
}

// Handler processes an inbound envelope on a specific channel.
type Handler func(env wire.Envelope) error

// Client is the long-lived relay client. Construct with New, then call Run.
type Client struct {
	cfg      Config
	logger   *slog.Logger
	outbound chan wire.Envelope

	mu       sync.RWMutex
	handlers map[wire.Channel]Handler

	// connectedCh is closed when a session attaches; reopened on reconnect.
	connectedMu sync.Mutex
	connectedCh chan struct{}
	connected   bool
}

// New builds a client from cfg. AccessToken can be updated later via SetAccessToken.
func New(cfg Config, logger *slog.Logger) *Client {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		cfg:         cfg,
		logger:      logger,
		outbound:    make(chan wire.Envelope, 64),
		handlers:    make(map[wire.Channel]Handler),
		connectedCh: make(chan struct{}),
	}
	return c
}

// SetAccessToken swaps the bearer used for the next reconnect.
func (c *Client) SetAccessToken(tok string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.AccessToken = tok
}

// Handle registers a handler for a channel. If a handler is already registered
// for the channel, it is replaced.
func (c *Client) Handle(ch wire.Channel, h Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[ch] = h
}

// Send enqueues env for transmission to the relay. Oversized envelopes and
// full-buffer writes are logged and dropped so callers never block.
func (c *Client) Send(env wire.Envelope) bool {
	encoded, err := json.Marshal(env)
	if err != nil {
		c.logger.Warn("outbound envelope encode failed, dropping",
			"channel", env.Channel, "method", env.Method, "err", err)
		return false
	}
	if len(encoded) > wire.MaxEnvelopeBytes {
		c.logger.Warn("outbound envelope too large, dropping",
			"channel", env.Channel, "method", env.Method, "bytes", len(encoded))
		return false
	}
	select {
	case c.outbound <- env:
		return true
	default:
		c.logger.Warn("outbound buffer full, dropping",
			"channel", env.Channel, "method", env.Method)
		return false
	}
}

// IsConnected reports whether a session is currently attached.
func (c *Client) IsConnected() bool {
	c.connectedMu.Lock()
	defer c.connectedMu.Unlock()
	return c.connected
}

// WaitConnected blocks until the client is connected or ctx is cancelled.
func (c *Client) WaitConnected(ctx context.Context) error {
	c.connectedMu.Lock()
	if c.connected {
		c.connectedMu.Unlock()
		return nil
	}
	ch := c.connectedCh
	c.connectedMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// Run keeps a session attached until ctx is cancelled, reconnecting with
// capped exponential backoff between attempts. A session that stayed up for
// at least BackoffResetAfter resets the backoff before the next attempt.
func (c *Client) Run(ctx context.Context) error {
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := time.Now()
		err := c.session(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		c.markDisconnected()
		if time.Since(start) >= c.cfg.BackoffResetAfter {
			attempt = 0
		}
		if err != nil {
			c.logger.Warn("relay session ended", "err", err, "attempt", attempt)
		}
		delay := backoff(attempt, c.cfg.MaxBackoff)
		attempt++
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// session opens one WebSocket and runs read/write/ping loops until any of
// them returns. The first error wins.
func (c *Client) session(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.HandshakeTimeout)
	defer cancel()

	c.mu.RLock()
	tok := c.cfg.AccessToken
	c.mu.RUnlock()

	hdr := http.Header{}
	if tok != "" {
		hdr.Set("Authorization", "Bearer "+tok)
	}

	url := c.relayWSURL()
	c.logger.Info("relay dialing", "url", url)
	ws, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{
		HTTPHeader: hdr,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	ws.SetReadLimit(wire.MaxEnvelopeBytes)

	c.markConnected()
	c.logger.Info("relay connected")

	defer func() {
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}()

	sessCtx, cancelSess := context.WithCancel(ctx)
	defer cancelSess()

	errCh := make(chan error, 3)

	go func() { errCh <- c.readLoop(sessCtx, ws) }()
	go func() { errCh <- c.writeLoop(sessCtx, ws) }()
	go func() { errCh <- c.pingLoop(sessCtx) }()

	err = <-errCh
	cancelSess()
	// Drain remaining errors from the other goroutines so they don't leak.
	for i := 0; i < 2; i++ {
		<-errCh
	}
	return err
}

func (c *Client) readLoop(ctx context.Context, ws *websocket.Conn) error {
	for {
		var env wire.Envelope
		if err := wsjson.Read(ctx, ws, &env); err != nil {
			return err
		}
		c.dispatch(env)
	}
}

func (c *Client) writeLoop(ctx context.Context, ws *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case env := <-c.outbound:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(writeCtx, ws, env)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func (c *Client) pingLoop(ctx context.Context) error {
	t := time.NewTicker(c.cfg.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			ping, err := wire.MakeRequest(
				fmt.Sprintf("ping-%d", time.Now().UnixNano()),
				wire.ChannelMeta, "ping",
				map[string]string{"nonce": fmt.Sprintf("%d", time.Now().UnixNano())},
			)
			if err != nil {
				continue
			}
			c.Send(ping)
		}
	}
}

func (c *Client) dispatch(env wire.Envelope) {
	c.mu.RLock()
	h, ok := c.handlers[env.Channel]
	c.mu.RUnlock()
	if !ok {
		c.logger.Debug("dropping unhandled envelope",
			"channel", env.Channel, "method", env.Method, "type", env.Type)
		return
	}
	if err := h(env); err != nil {
		c.logger.Warn("handler error", "channel", env.Channel, "err", err)
	}
}

func (c *Client) relayWSURL() string {
	base := strings.TrimRight(c.cfg.RelayURL, "/")
	switch {
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	}
	return base + "/v1/ws/connect"
}

func (c *Client) markConnected() {
	c.connectedMu.Lock()
	defer c.connectedMu.Unlock()
	c.connected = true
	close(c.connectedCh)
}

func (c *Client) markDisconnected() {
	c.connectedMu.Lock()
	defer c.connectedMu.Unlock()
	if !c.connected {
		return
	}
	c.connected = false
	c.connectedCh = make(chan struct{})
}

// backoff returns a jittered exponential backoff delay for `attempt`,
// capped at max. The first attempt has a small initial delay so back-to-back
// failures aren't truly tight-loop.
func backoff(attempt int, max time.Duration) time.Duration {
	const base = 250 * time.Millisecond
	if max <= 0 {
		max = 30 * time.Second
	}
	d := base * (1 << uint(attempt))
	if d <= 0 || d > max {
		d = max
	}
	// Apply +/- 20% jitter.
	jitter := time.Duration(randomInt64(int64(d) / 5))
	if randomInt64(2) == 0 {
		return d - jitter
	}
	return d + jitter
}

func randomInt64(max int64) int64 {
	if max <= 0 {
		return 0
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return n.Int64()
}
