// Package diffrouter wires the daemon's `diff` channel onto internal/diffapi.
package diffrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Mutech-Innovation/coding-anywhere/internal/diffapi"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

// Router answers diff.* envelopes for a fixed workspace.
type Router struct {
	cli    *relayclient.Client
	logger *slog.Logger
	api    *diffapi.API
}

func New(cli *relayclient.Client, logger *slog.Logger, workspaceRoot string) (*Router, error) {
	api, err := diffapi.New(workspaceRoot)
	if err != nil {
		return nil, err
	}
	r := &Router{cli: cli, logger: logger, api: api}
	cli.Handle(wire.ChannelGit, r.handle) // diff lives on the `git` channel
	return r, nil
}

func (r *Router) handle(env wire.Envelope) error {
	if env.Type != wire.TypeRequest {
		return nil
	}
	ctx := context.Background()
	switch env.Method {
	case wire.DiffMethodListChanges:
		isRepo := r.api.IsRepo(ctx)
		if !isRepo {
			r.respondOK(env, wire.DiffListChangesResponse{IsRepo: false})
			return nil
		}
		ch, err := r.api.ListChanges(ctx)
		if err != nil {
			r.respondErr(env, "list_failed", err.Error())
			return nil
		}
		out := make([]wire.DiffChange, 0, len(ch))
		for _, c := range ch {
			out = append(out, wire.DiffChange{
				Path:    c.Path,
				OldPath: c.OldPath,
				Kind:    wire.DiffChangeKind(c.Kind),
				Staged:  c.Staged,
			})
		}
		r.respondOK(env, wire.DiffListChangesResponse{Changes: out, IsRepo: true})
	case wire.DiffMethodVersions:
		var req wire.DiffVersionsRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		original, current, err := r.api.Versions(ctx, req.Path)
		if err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		r.respondOK(env, wire.DiffVersionsResponse{
			Path:     req.Path,
			Original: original,
			Current:  current,
		})
	default:
		r.respondErr(env, "unknown_method", fmt.Sprintf("unknown diff method %q", env.Method))
	}
	return nil
}

// --- helpers ---

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

func classifyErr(err error) string {
	if errors.Is(err, diffapi.ErrNotARepo) {
		return "not_a_repo"
	}
	return "internal_error"
}
