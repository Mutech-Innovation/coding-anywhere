package acp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/acp"
)

// pipePair returns two pipes wired so:
//   - clientReader/clientWriter is the client's view (we feed it server→client
//     bytes via clientWriter and read client→server bytes via clientReader).
//   - The "server" side is the mock; reads from cReader = client's writes.
type pipePair struct {
	cReader  *io.PipeReader // client reads here
	cWriter  *io.PipeWriter // client writes here
	cReaderW *io.PipeWriter // server writes into client's reader from here
	cWriterR *io.PipeReader // server reads what client wrote
}

func newPipePair() *pipePair {
	cR, sW := io.Pipe()
	sR, cW := io.Pipe()
	return &pipePair{cReader: cR, cReaderW: sW, cWriterR: sR, cWriter: cW}
}

// asClient returns the (reader, writer) the Client should use.
func (p *pipePair) asClient() (io.Reader, io.Writer) {
	return p.cReader, p.cWriter
}

// serverWrite sends a JSON-RPC frame to the client.
func (p *pipePair) serverWrite(t *testing.T, msg string) {
	t.Helper()
	if _, err := io.WriteString(p.cReaderW, msg+"\n"); err != nil {
		t.Fatalf("serverWrite: %v", err)
	}
}

// serverReadFrame reads one frame written by the client.
func (p *pipePair) serverReadFrame(t *testing.T, timeout time.Duration) string {
	t.Helper()
	buf := make([]byte, 4096)
	type res struct {
		s   string
		err error
	}
	out := make(chan res, 1)
	go func() {
		n, err := p.cWriterR.Read(buf)
		out <- res{s: string(buf[:n]), err: err}
	}()
	select {
	case r := <-out:
		if r.err != nil && !errors.Is(r.err, io.EOF) {
			t.Fatalf("serverRead: %v", r.err)
		}
		return strings.TrimRight(r.s, "\n")
	case <-time.After(timeout):
		t.Fatal("serverRead timeout")
	}
	return ""
}

func (p *pipePair) close() {
	_ = p.cReaderW.Close()
	_ = p.cWriter.Close()
}

func TestACPInitialize(t *testing.T) {
	pp := newPipePair()
	r, w := pp.asClient()

	cli := acp.New(r, w, acp.Options{})
	go func() { _ = cli.Run(context.Background()) }()
	t.Cleanup(func() {
		_ = cli.Close()
		pp.close()
	})

	// Send the initialize call from a goroutine and capture the result.
	done := make(chan error, 1)
	var resp acp.InitializeResponse
	go func() {
		done <- cli.Call(context.Background(), acp.MethodInitialize,
			acp.InitializeRequest{
				ProtocolVersion: 1,
				ClientInfo:      acp.ImplementationInfo{Name: "test-client"},
			}, &resp)
	}()

	// Read what the client sent.
	frame := pp.serverReadFrame(t, 2*time.Second)
	if !strings.Contains(frame, `"method":"initialize"`) {
		t.Fatalf("unexpected frame: %s", frame)
	}
	// Reply with a synthetic initialize result.
	pp.serverWrite(t, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`)

	if err := <-done; err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.ProtocolVersion != 1 {
		t.Fatalf("protocolVersion: %d", resp.ProtocolVersion)
	}
	if !resp.AgentCapabilities.LoadSession {
		t.Fatalf("loadSession not parsed")
	}
}

func TestACPCallReturnsRPCError(t *testing.T) {
	pp := newPipePair()
	r, w := pp.asClient()
	cli := acp.New(r, w, acp.Options{})
	go func() { _ = cli.Run(context.Background()) }()
	t.Cleanup(func() {
		_ = cli.Close()
		pp.close()
	})

	done := make(chan error, 1)
	go func() {
		done <- cli.Call(context.Background(), "session/new", map[string]any{"cwd": "/tmp"}, nil)
	}()

	_ = pp.serverReadFrame(t, 2*time.Second)
	pp.serverWrite(t, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"bad params"}}`)

	err := <-done
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected *RPCError, got %T %v", err, err)
	}
	if rpcErr.Code != -32602 {
		t.Fatalf("code: %d", rpcErr.Code)
	}
}

// TestRPCErrorIncludesData pins that the error string surfaces the JSON-RPC
// `data` payload. Agents hide the real cause there — cursor-agent answers
// authenticate failures with message "Internal error" and the underlying
// network error only in data (e.g. `{"details":"[unavailable] connect ..."}`),
// which the daemon log would otherwise drop.
func TestRPCErrorIncludesData(t *testing.T) {
	withData := &acp.RPCError{
		Code:    -32603,
		Message: "Internal error",
		Data:    json.RawMessage(`{"details":"[unavailable] connect ECONNREFUSED 127.0.0.1:9"}`),
	}
	if got := withData.Error(); !strings.Contains(got, "[unavailable] connect ECONNREFUSED 127.0.0.1:9") {
		t.Fatalf("data not surfaced: %s", got)
	}
	bare := &acp.RPCError{Code: -32602, Message: "bad params"}
	if got := bare.Error(); got != "rpc error -32602: bad params" {
		t.Fatalf("bare error format changed: %s", got)
	}
}

func TestACPNotificationDelivery(t *testing.T) {
	pp := newPipePair()
	r, w := pp.asClient()
	var (
		mu        sync.Mutex
		gotMethod string
		gotParams string
	)
	gotCh := make(chan struct{}, 1)
	cli := acp.New(r, w, acp.Options{
		OnNotification: func(method string, params json.RawMessage) {
			mu.Lock()
			gotMethod = method
			gotParams = string(params)
			mu.Unlock()
			select {
			case gotCh <- struct{}{}:
			default:
			}
		},
	})
	go func() { _ = cli.Run(context.Background()) }()
	t.Cleanup(func() {
		_ = cli.Close()
		pp.close()
	})

	pp.serverWrite(t, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk"}}}`)
	select {
	case <-gotCh:
	case <-time.After(2 * time.Second):
		t.Fatal("notification not received")
	}
	mu.Lock()
	defer mu.Unlock()
	if gotMethod != "session/update" {
		t.Fatalf("method: %s", gotMethod)
	}
	if !strings.Contains(gotParams, `"sessionId":"s1"`) {
		t.Fatalf("params: %s", gotParams)
	}
}

func TestACPIncomingRequest(t *testing.T) {
	pp := newPipePair()
	r, w := pp.asClient()
	cli := acp.New(r, w, acp.Options{
		OnRequest: func(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *acp.RPCError) {
			if method != "session/request_permission" {
				return nil, &acp.RPCError{Code: -32601, Message: "unsupported"}
			}
			return json.RawMessage(`{"decision":"allow"}`), nil
		},
	})
	go func() { _ = cli.Run(context.Background()) }()
	t.Cleanup(func() {
		_ = cli.Close()
		pp.close()
	})

	pp.serverWrite(t, `{"jsonrpc":"2.0","id":42,"method":"session/request_permission","params":{}}`)

	// Read the response the client sent back.
	frame := pp.serverReadFrame(t, 2*time.Second)
	if !strings.Contains(frame, `"id":42`) {
		t.Fatalf("frame: %s", frame)
	}
	if !strings.Contains(frame, `"decision":"allow"`) {
		t.Fatalf("frame: %s", frame)
	}
}

func TestACPCloseUnblocksPending(t *testing.T) {
	pp := newPipePair()
	r, w := pp.asClient()
	cli := acp.New(r, w, acp.Options{})
	go func() { _ = cli.Run(context.Background()) }()

	done := make(chan error, 1)
	go func() {
		done <- cli.Call(context.Background(), "session/new", map[string]string{"cwd": "/tmp"}, nil)
	}()

	// Discard the request frame so the goroutine doesn't block on writes.
	_ = pp.serverReadFrame(t, 2*time.Second)
	_ = cli.Close()
	pp.close()

	select {
	case err := <-done:
		// We accept either the wrapping RPCError pushed onto resCh OR
		// ErrClientClosed if the doneCh select arm fired first — either
		// reliably terminates the in-flight Call with an error.
		if err == nil {
			t.Fatal("expected error after Close, got nil")
		}
		var rpcErr *acp.RPCError
		if !errors.As(err, &rpcErr) && !errors.Is(err, acp.ErrClientClosed) {
			t.Fatalf("unexpected error type after Close: %T %v", err, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after Close")
	}

	if err := cli.Notify("session/cancel", map[string]string{"sessionId": "s"}); !errors.Is(err, acp.ErrClientClosed) {
		t.Fatalf("Notify after close: %v", err)
	}
}

func TestACPContentBlockRoundTrip(t *testing.T) {
	in := acp.ContentBlock{
		Type: "text",
		Text: "hi",
		Extra: map[string]any{
			"annotations": map[string]any{"audience": "user"},
		},
	}
	buf, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), `"audience":"user"`) {
		t.Fatalf("extra not preserved: %s", buf)
	}
	var out acp.ContentBlock
	if err := json.Unmarshal(buf, &out); err != nil {
		t.Fatal(err)
	}
	if out.Type != "text" || out.Text != "hi" {
		t.Fatalf("typed fields lost: %+v", out)
	}
	if out.Extra["annotations"] == nil {
		t.Fatalf("extra dropped: %+v", out.Extra)
	}
}
