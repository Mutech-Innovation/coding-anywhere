// Package portsrouter wires the daemon's `ports` channel onto internal/portsapi.
package portsrouter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Mutech-Innovation/coding-anywhere/internal/portsapi"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

// Router answers ports.list / ports.fetch envelopes.
type Router struct {
	cli    *relayclient.Client
	logger *slog.Logger
	api    *portsapi.API
}

// New constructs a router and registers a `ports` channel handler.
func New(cli *relayclient.Client, logger *slog.Logger) *Router {
	r := &Router{cli: cli, logger: logger, api: portsapi.New()}
	cli.Handle(wire.ChannelPorts, r.handle)
	return r
}

func (r *Router) handle(env wire.Envelope) error {
	if env.Type != wire.TypeRequest {
		return nil
	}
	switch env.Method {
	case wire.PortsMethodList:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ports, err := r.api.ListListening(ctx)
		if err != nil {
			r.respondErr(env, "list_failed", err.Error())
			return nil
		}
		entries := make([]wire.PortsEntry, 0, len(ports))
		for _, p := range ports {
			entries = append(entries, wire.PortsEntry{
				Port:    p.Port,
				Address: p.Address,
				Family:  p.Family,
				PID:     p.PID,
				Command: p.Command,
			})
		}
		r.respondOK(env, wire.PortsListResponse{Entries: entries})
	case wire.PortsMethodFetch:
		var req wire.PortsFetchRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		r.handleFetch(env, req)
	default:
		r.respondErr(env, "unknown_method", fmt.Sprintf("unknown ports method %q", env.Method))
	}
	return nil
}

func (r *Router) handleFetch(env wire.Envelope, req wire.PortsFetchRequest) {
	body, err := base64.StdEncoding.DecodeString(req.Body)
	if err != nil {
		r.respondErr(env, "bad_payload", err.Error())
		return
	}
	headers := http.Header{}
	for _, h := range req.Headers {
		headers.Add(h.Name, h.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := r.api.Fetch(ctx, portsapi.FetchRequest{
		Port:         req.Port,
		Family:       req.Family,
		Method:       req.Method,
		Path:         req.Path,
		Headers:      headers,
		Body:         body,
		MaxBodyBytes: req.MaxBodyBytes,
	})
	if err != nil {
		r.respondErr(env, "fetch_failed", err.Error())
		return
	}
	out := wire.PortsFetchResponse{
		Status:     res.Status,
		Body:       base64.StdEncoding.EncodeToString(res.Body),
		Truncated:  res.Truncated,
		UpstreamMS: res.UpstreamMS,
	}
	for k, vs := range res.Headers {
		for _, v := range vs {
			out.Headers = append(out.Headers, wire.PortsHeader{Name: k, Value: v})
		}
	}
	r.respondOK(env, out)
}

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
