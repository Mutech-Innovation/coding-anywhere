// Package terminalrouter wires the daemon's PTY manager (internal/ptymux)
// onto the `terminal` channel of the relay's WebSocket transport.
//
// Per-method behaviour:
//   - terminal.open       → spawn shell, start streaming output events.
//   - terminal.list       → metadata for every running session.
//   - terminal.attach     → return scrollback snapshot + info (no-op on
//                           daemon state; chunks are already broadcast).
//   - terminal.detach     → no-op (clients may stop rendering at will).
//   - terminal.input      → write base64 bytes into the PTY.
//   - terminal.resize     → propagate to the PTY (TUIs see SIGWINCH).
//   - terminal.close      → kill the session.
//
// The relay's hub fans daemon→client envelopes to every connected device on
// the same account, so we only need to emit each event once.
package terminalrouter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/Mutech-Innovation/coding-anywhere/internal/ptymux"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
	"github.com/oklog/ulid/v2"
)

// Router owns the PTY manager and a relay client.
type Router struct {
	mgr      *ptymux.Manager
	cli      *relayclient.Client
	logger   *slog.Logger
	streamed sync.Map // session id → struct{} so we don't double-stream
	eventSeq atomic.Uint64
}

// New constructs a router and registers a `terminal` channel handler.
func New(cli *relayclient.Client, logger *slog.Logger) *Router {
	r := &Router{
		mgr:    ptymux.NewManager(),
		cli:    cli,
		logger: logger,
	}
	cli.Handle(wire.ChannelTerminal, r.handle)
	return r
}

// Manager exposes the underlying ptymux.Manager (used by the daemon to clean
// up on shutdown).
func (r *Router) Manager() *ptymux.Manager { return r.mgr }

// CloseAll terminates every session.
func (r *Router) CloseAll() error { return r.mgr.CloseAll() }

func (r *Router) handle(env wire.Envelope) error {
	if env.Type != wire.TypeRequest {
		return nil // ignore replies/events from peer
	}
	switch env.Method {
	case wire.TerminalMethodOpen:
		var req wire.TerminalOpenRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleOpen(env, req)
	case wire.TerminalMethodList:
		r.handleList(env)
	case wire.TerminalMethodAttach:
		var req wire.TerminalAttachRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleAttach(env, req)
	case wire.TerminalMethodDetach:
		r.respondOK(env, wire.TerminalAck{OK: true})
	case wire.TerminalMethodInput:
		var req wire.TerminalInputRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleInput(env, req)
	case wire.TerminalMethodResize:
		var req wire.TerminalResizeRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleResize(env, req)
	case wire.TerminalMethodClose:
		var req wire.TerminalCloseRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleClose(env, req)
	default:
		r.respondErr(env, "unknown_method", fmt.Sprintf("unknown terminal method %q", env.Method))
	}
	return nil
}

func (r *Router) handleOpen(env wire.Envelope, req wire.TerminalOpenRequest) {
	s, err := r.mgr.Open(ptymux.SessionOptions{
		Command: req.Command,
		Cwd:     req.Cwd,
		Cols:    req.Cols,
		Rows:    req.Rows,
	})
	if err != nil {
		r.respondErr(env, "open_failed", err.Error())
		return
	}
	info := toWireInfo(s.Info())
	r.respondOK(env, wire.TerminalOpenResponse{Session: info})
	r.startStreaming(s)
	r.broadcastEvent(wire.TerminalEventOpened, wire.TerminalOpenedEvent{Session: info})
}

func (r *Router) handleList(env wire.Envelope) {
	infos := r.mgr.List()
	out := make([]wire.TerminalSessionInfo, 0, len(infos))
	for _, i := range infos {
		out = append(out, toWireInfo(i))
	}
	r.respondOK(env, wire.TerminalListResponse{Sessions: out})
}

func (r *Router) handleAttach(env wire.Envelope, req wire.TerminalAttachRequest) {
	s, err := r.mgr.Get(req.ID)
	if err != nil {
		r.respondErr(env, "not_found", err.Error())
		return
	}
	// Pull a one-shot snapshot. We don't add a persistent subscriber here
	// because the streaming goroutine started at open-time already pushes
	// data to every connected client.
	_, snapshot, _, detach := s.Subscribe(1)
	detach()
	r.respondOK(env, wire.TerminalAttachResponse{
		Session:  toWireInfo(s.Info()),
		Snapshot: base64.StdEncoding.EncodeToString(snapshot),
	})
}

func (r *Router) handleInput(env wire.Envelope, req wire.TerminalInputRequest) {
	s, err := r.mgr.Get(req.ID)
	if err != nil {
		r.respondErr(env, "not_found", err.Error())
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		r.respondErr(env, "bad_payload", err.Error())
		return
	}
	if _, err := s.Write(data); err != nil {
		r.respondErr(env, "write_failed", err.Error())
		return
	}
	r.respondOK(env, wire.TerminalAck{OK: true})
}

func (r *Router) handleResize(env wire.Envelope, req wire.TerminalResizeRequest) {
	s, err := r.mgr.Get(req.ID)
	if err != nil {
		r.respondErr(env, "not_found", err.Error())
		return
	}
	if err := s.Resize(req.Cols, req.Rows); err != nil {
		r.respondErr(env, "resize_failed", err.Error())
		return
	}
	r.respondOK(env, wire.TerminalAck{OK: true})
}

func (r *Router) handleClose(env wire.Envelope, req wire.TerminalCloseRequest) {
	if err := r.mgr.Close(req.ID); err != nil {
		if errors.Is(err, ptymux.ErrNotFound) {
			r.respondErr(env, "not_found", err.Error())
			return
		}
		r.respondErr(env, "close_failed", err.Error())
		return
	}
	r.respondOK(env, wire.TerminalAck{OK: true})
}

// startStreaming subscribes to a session and forwards every PTY chunk as a
// terminal.data event on the relay. When the session terminates, emits a
// terminal.closed event with the exit code.
func (r *Router) startStreaming(s *ptymux.Session) {
	if _, loaded := r.streamed.LoadOrStore(s.ID, struct{}{}); loaded {
		return
	}
	_, _, ch, detach := s.Subscribe(1024)
	go func() {
		defer detach()
		for chunk := range ch {
			if len(chunk) == 0 {
				continue
			}
			r.broadcastEvent(wire.TerminalEventData, wire.TerminalDataEvent{
				ID:   s.ID,
				Data: base64.StdEncoding.EncodeToString(chunk),
			})
		}
		// Channel closed → session is over. The exit event is already in
		// the buffered exit channel (waitLoop writes before tearing down
		// subscribers), so this Wait returns immediately.
		exit, _ := s.Wait(context.Background())
		errMsg := ""
		if exit.Err != nil {
			errMsg = exit.Err.Error()
		}
		r.broadcastEvent(wire.TerminalEventClosed, wire.TerminalClosedEvent{
			ID:       s.ID,
			ExitCode: exit.Code,
			Err:      errMsg,
		})
		r.streamed.Delete(s.ID)
	}()
}

// --- response helpers ---

func (r *Router) parse(env wire.Envelope, out any) error {
	if len(env.Payload) == 0 {
		return errors.New("empty payload")
	}
	if err := json.Unmarshal(env.Payload, out); err != nil {
		r.respondErr(env, "bad_payload", err.Error())
		return err
	}
	return nil
}

func (r *Router) respondOK(env wire.Envelope, payload any) {
	res, err := wire.MakeResponse(env.ID, env.Channel, payload, nil)
	if err != nil {
		r.logger.Warn("respondOK encode", "err", err)
		return
	}
	r.cli.Send(res)
}

func (r *Router) respondErr(env wire.Envelope, code, msg string) {
	res, err := wire.MakeResponse(env.ID, env.Channel, nil, &wire.EnvelopeError{
		Code: code, Message: msg,
	})
	if err != nil {
		return
	}
	r.cli.Send(res)
}

func (r *Router) broadcastEvent(method string, payload any) {
	r.eventSeq.Add(1)
	id := fmt.Sprintf("%s-%d", ulid.Make().String(), r.eventSeq.Load())
	ev, err := wire.MakeEvent(id, wire.ChannelTerminal, method, payload)
	if err != nil {
		r.logger.Warn("MakeEvent", "method", method, "err", err)
		return
	}
	r.cli.Send(ev)
}

func toWireInfo(i ptymux.SessionInfo) wire.TerminalSessionInfo {
	return wire.TerminalSessionInfo{
		ID:        i.ID,
		PID:       i.PID,
		Cols:      i.Cols,
		Rows:      i.Rows,
		Command:   i.Command,
		Cwd:       i.Cwd,
		StartedAt: i.StartedAt,
	}
}


