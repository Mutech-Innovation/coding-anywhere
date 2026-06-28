// Package filesrouter wires the daemon's `files` WebSocket channel onto
// internal/fileapi. For MVP the daemon serves exactly one workspace; v2
// can introduce multi-workspace bookkeeping when the workspace UX in the
// web client is fleshed out.
package filesrouter

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/Mutech-Innovation/coding-anywhere/internal/fileapi"
	"github.com/Mutech-Innovation/coding-anywhere/internal/relayclient"
	"github.com/Mutech-Innovation/coding-anywhere/internal/wire"
)

// Router holds one workspace and answers files.* envelopes.
type Router struct {
	cli       *relayclient.Client
	logger    *slog.Logger
	api       *fileapi.API
	workspace wire.FilesWorkspaceInfo
}

// New constructs a router rooted at `workspaceRoot` and registers a `files`
// channel handler on the relay client. The first id is "default".
func New(cli *relayclient.Client, logger *slog.Logger, workspaceRoot string) (*Router, error) {
	api, err := fileapi.New(workspaceRoot)
	if err != nil {
		return nil, err
	}
	r := &Router{
		cli: cli, logger: logger, api: api,
		workspace: wire.FilesWorkspaceInfo{
			ID:   "default",
			Name: filepath.Base(api.Root()),
			Root: api.Root(),
		},
	}
	cli.Handle(wire.ChannelFiles, r.handle)
	return r, nil
}

// Workspace returns the static workspace descriptor.
func (r *Router) Workspace() wire.FilesWorkspaceInfo { return r.workspace }

func (r *Router) handle(env wire.Envelope) error {
	if env.Type != wire.TypeRequest {
		return nil
	}
	switch env.Method {
	case wire.FilesMethodListWorkspace:
		r.respondOK(env, wire.FilesListWorkspaceResponse{
			Workspaces: []wire.FilesWorkspaceInfo{r.workspace},
		})
	case wire.FilesMethodList:
		var req wire.FilesListRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		entries, err := r.api.List(req.Path, fileapi.ListOptions{
			IncludeHidden: req.IncludeHidden,
			MaxEntries:    req.MaxEntries,
		})
		if err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		out := make([]wire.FilesEntry, 0, len(entries))
		for _, e := range entries {
			out = append(out, toWireEntry(e))
		}
		r.respondOK(env, wire.FilesListResponse{
			WorkspaceID: r.workspace.ID,
			Path:        req.Path,
			Entries:     out,
		})
	case wire.FilesMethodRead:
		var req wire.FilesReadRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		res, err := r.api.Read(req.Path, fileapi.ReadOptions{
			MaxBytes:    req.MaxBytes,
			Offset:      req.Offset,
			Length:      req.Length,
			AllowBinary: req.AllowBinary,
		})
		if err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		r.respondOK(env, wire.FilesReadResponse{
			WorkspaceID: r.workspace.ID,
			Path:        res.Path,
			Size:        res.Size,
			Truncated:   res.Truncated,
			IsBinary:    res.IsBinary,
			ContentType: res.ContentType,
			Data:        base64.StdEncoding.EncodeToString(res.Bytes),
		})
	case wire.FilesMethodStat:
		var req wire.FilesStatRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		entry, err := r.api.Stat(req.Path)
		if err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		r.respondOK(env, wire.FilesStatResponse{
			WorkspaceID: r.workspace.ID,
			Entry:       toWireEntry(entry),
		})
	case wire.FilesMethodWrite:
		var req wire.FilesWriteRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		data, err := base64.StdEncoding.DecodeString(req.Data)
		if err != nil {
			r.respondErr(env, "bad_payload", "data is not valid base64")
			return nil
		}
		size, err := r.api.Write(req.Path, data, fileapi.WriteOptions{
			Offset:   req.Offset,
			Truncate: req.Truncate,
		})
		if err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		r.respondOK(env, wire.FilesWriteResponse{
			WorkspaceID: r.workspace.ID,
			Path:        req.Path,
			Size:        size,
		})
	case wire.FilesMethodDelete:
		var req wire.FilesDeleteRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		if err := r.api.Remove(req.Path); err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		r.respondOK(env, wire.FilesDeleteResponse{
			WorkspaceID: r.workspace.ID,
			Path:        req.Path,
		})
	case wire.FilesMethodMove:
		var req wire.FilesMoveRequest
		if err := r.parse(env, &req); err != nil {
			return nil
		}
		if err := r.api.Move(req.From, req.To); err != nil {
			r.respondErr(env, classifyErr(err), err.Error())
			return nil
		}
		r.respondOK(env, wire.FilesMoveResponse{
			WorkspaceID: r.workspace.ID,
			From:        req.From,
			To:          req.To,
		})
	default:
		r.respondErr(env, "unknown_method", fmt.Sprintf("unknown files method %q", env.Method))
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

func toWireEntry(e fileapi.Entry) wire.FilesEntry {
	return wire.FilesEntry{
		Name:    e.Name,
		Path:    e.Path,
		Kind:    wire.FilesEntryKind(e.Kind),
		Size:    e.Size,
		ModTime: e.ModTime,
		Target:  e.Target,
	}
}

func classifyErr(err error) string {
	if errors.Is(err, fileapi.ErrEscapesRoot) {
		return "forbidden"
	}
	if err != nil {
		s := err.Error()
		if strings.Contains(s, "already exists") {
			return "conflict"
		}
		if strings.Contains(s, "is a directory") {
			return "bad_request"
		}
		for _, needle := range []string{"no such file", "does not exist", "not a directory"} {
			if strings.Contains(s, needle) {
				return "not_found"
			}
		}
	}
	return "internal_error"
}
