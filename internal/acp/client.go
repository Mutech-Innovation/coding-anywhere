package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Client is a JSON-RPC 2.0 client that speaks ACP over an io.Reader/Writer
// pair. Construct with New, then Run() to start the read loop, then call
// Initialize and the per-session methods.
//
// Concurrency: Client is safe for concurrent use. Multiple goroutines can
// call Call/Notify simultaneously; in-flight requests are correlated by id.
type Client struct {
	rw       *bufio.ReadWriter
	logger   *slog.Logger
	writeMu  sync.Mutex
	pendMu   sync.Mutex
	pending  map[int64]*pending
	idSeq    atomic.Int64
	notify   func(method string, params json.RawMessage)
	request  func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *RPCError)
	closed   atomic.Bool
	doneCh   chan struct{}
	doneErr  error
	doneOnce sync.Once
}

// pending holds the channel that receives the response (or error) for one
// in-flight request.
type pending struct {
	resCh chan rpcResult
}

type rpcResult struct {
	body json.RawMessage
	err  *RPCError
}

// RPCError mirrors the JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return ""
	}
	// Surface `data` when present: agents often hide the actual cause
	// there. cursor-agent's authenticate failure is the canonical case —
	// `message` is just "Internal error" while data carries e.g.
	// `{"details":"[unavailable] connect ECONNREFUSED ..."}`.
	if len(e.Data) > 0 {
		return fmt.Sprintf("rpc error %d: %s (data: %s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// rpcMessage is the union of request, response, and notification used for
// reading. We emit only the right subset on writes.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Options configure a new Client.
type Options struct {
	Logger *slog.Logger
	// OnNotification handles unsolicited notifications from the agent
	// (most importantly session/update). May be called from the read loop;
	// callbacks should not block for long.
	OnNotification func(method string, params json.RawMessage)
	// OnRequest handles incoming requests from the agent (e.g.
	// session/request_permission). Must return a result body or RPCError.
	// May be nil — if so, all incoming requests are answered with an
	// MethodNotFound error.
	OnRequest func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *RPCError)
}

// New constructs a Client that reads from r and writes to w. Both ends are
// expected to be the stdio of an ACP-speaking subprocess.
func New(r io.Reader, w io.Writer, opts Options) *Client {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		rw:      bufio.NewReadWriter(bufio.NewReader(r), bufio.NewWriter(w)),
		logger:  opts.Logger,
		pending: make(map[int64]*pending),
		notify:  opts.OnNotification,
		request: opts.OnRequest,
		doneCh:  make(chan struct{}),
	}
}

// Run starts the read loop. It blocks until the underlying reader returns
// io.EOF / an error, or ctx is cancelled. Any in-flight callers are unblocked
// with ErrClientClosed when Run exits.
func (c *Client) Run(ctx context.Context) error {
	defer c.markDone(nil)

	dec := json.NewDecoder(c.rw.Reader)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var msg rpcMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			c.markDone(err)
			return err
		}
		c.dispatch(ctx, msg)
	}
}

func (c *Client) dispatch(ctx context.Context, msg rpcMessage) {
	switch {
	case len(msg.ID) > 0 && (msg.Result != nil || msg.Error != nil):
		// It's a response.
		c.handleResponse(msg)
	case len(msg.ID) > 0 && msg.Method != "":
		// Incoming request from the agent.
		c.handleRequest(ctx, msg)
	case msg.Method != "":
		// Notification.
		if c.notify != nil {
			c.notify(msg.Method, msg.Params)
		}
	default:
		c.logger.Warn("acp: unrecognised message", "msg", string(mustMarshal(msg)))
	}
}

func (c *Client) handleResponse(msg rpcMessage) {
	var idNum int64
	if err := json.Unmarshal(msg.ID, &idNum); err != nil {
		c.logger.Warn("acp: response with non-numeric id", "id", string(msg.ID))
		return
	}
	c.pendMu.Lock()
	p, ok := c.pending[idNum]
	if ok {
		delete(c.pending, idNum)
	}
	c.pendMu.Unlock()
	if !ok {
		c.logger.Debug("acp: response for unknown id", "id", idNum)
		return
	}
	p.resCh <- rpcResult{body: msg.Result, err: msg.Error}
}

func (c *Client) handleRequest(ctx context.Context, msg rpcMessage) {
	if c.request == nil {
		_ = c.writeRaw(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Error:   &RPCError{Code: -32601, Message: "method not found"},
		})
		return
	}
	go func() {
		body, rpcErr := c.request(ctx, msg.Method, msg.Params)
		out := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
		if rpcErr != nil {
			out.Error = rpcErr
		} else {
			out.Result = body
		}
		_ = c.writeRaw(out)
	}()
}

// Call invokes `method` on the agent and waits for its response. Returns
// the result bytes (caller json-decodes) or an *RPCError.
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	if c.closed.Load() {
		return ErrClientClosed
	}
	id := c.idSeq.Add(1)
	idJSON, _ := json.Marshal(id)
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return err
	}

	p := &pending{resCh: make(chan rpcResult, 1)}
	c.pendMu.Lock()
	c.pending[id] = p
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
	}()

	if err := c.writeRaw(rpcMessage{
		JSONRPC: "2.0", ID: idJSON, Method: method, Params: paramsJSON,
	}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.doneCh:
		return c.terminationError()
	case r := <-p.resCh:
		if r.err != nil {
			return r.err
		}
		if out != nil && len(r.body) > 0 {
			if err := json.Unmarshal(r.body, out); err != nil {
				return fmt.Errorf("decode result: %w", err)
			}
		}
		return nil
	}
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(method string, params any) error {
	if c.closed.Load() {
		return ErrClientClosed
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.writeRaw(rpcMessage{JSONRPC: "2.0", Method: method, Params: paramsJSON})
}

// Close terminates the client and unblocks all in-flight callers. It does
// not close the underlying io.ReadWriter — the agent subprocess wrapper
// is responsible for that.
func (c *Client) Close() error {
	c.markDone(ErrClientClosed)
	return nil
}

// Done returns a channel that's closed when the client is no longer running.
func (c *Client) Done() <-chan struct{} { return c.doneCh }

// terminationError returns the error that caused the client to stop, or
// ErrClientClosed if it was Close()'d cleanly.
func (c *Client) terminationError() error {
	if c.doneErr != nil {
		return c.doneErr
	}
	return ErrClientClosed
}

func (c *Client) markDone(err error) {
	c.doneOnce.Do(func() {
		c.closed.Store(true)
		c.doneErr = err
		close(c.doneCh)
		// Unblock any pending requests.
		c.pendMu.Lock()
		for _, p := range c.pending {
			select {
			case p.resCh <- rpcResult{err: &RPCError{Code: -32000, Message: "client closed"}}:
			default:
			}
		}
		c.pending = map[int64]*pending{}
		c.pendMu.Unlock()
	})
}

func (c *Client) writeRaw(msg rpcMessage) error {
	if c.closed.Load() {
		return ErrClientClosed
	}
	msg.JSONRPC = "2.0"
	buf, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.rw.Write(buf); err != nil {
		c.markDone(err)
		return err
	}
	if _, err := c.rw.Write([]byte{'\n'}); err != nil {
		c.markDone(err)
		return err
	}
	if err := c.rw.Flush(); err != nil {
		c.markDone(err)
		return err
	}
	return nil
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("<marshal error: " + err.Error() + ">")
	}
	return b
}

// ErrClientClosed is returned by Call/Notify after Close() or after the
// underlying transport ends.
var ErrClientClosed = errors.New("acp client closed")
