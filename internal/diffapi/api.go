// Package diffapi computes diff metadata + per-file before/after contents
// for a workspace using the local `git` binary.
//
// We shell out to git rather than embed a Go git library for two reasons:
//
//   - Robustness: real-world repos have submodules, unusual configs, ignore
//     rules, and rebase states. The git CLI is the canonical implementation.
//   - Simplicity: we only need the porcelain output and `git show` for
//     content; no need to read the object database directly.
//
// All operations are scoped to the workspace root and reject paths outside
// that root (delegated to fileapi).
package diffapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// API is workspace-scoped. Construct via New.
type API struct {
	root string
}

// New verifies the workspace is a git repo and returns an API rooted at it.
func New(workspaceRoot string) (*API, error) {
	if workspaceRoot == "" {
		return nil, errors.New("workspaceRoot required")
	}
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}
	return &API{root: resolved}, nil
}

// IsRepo reports whether the workspace is inside a git working tree.
func (a *API) IsRepo(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", a.root, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// ChangeKind is the type of change for a file.
type ChangeKind string

const (
	ChangeAdded     ChangeKind = "added"
	ChangeModified  ChangeKind = "modified"
	ChangeDeleted   ChangeKind = "deleted"
	ChangeRenamed   ChangeKind = "renamed"
	ChangeCopied    ChangeKind = "copied"
	ChangeUntracked ChangeKind = "untracked"
)

// Change is one entry in the workspace's change list.
type Change struct {
	Path    string     `json:"path"`
	OldPath string     `json:"old_path,omitempty"` // for renames/copies
	Kind    ChangeKind `json:"kind"`
	Staged  bool       `json:"staged,omitempty"`
}

// ListChanges returns the working-tree + staged + untracked file changes vs HEAD.
func (a *API) ListChanges(ctx context.Context) ([]Change, error) {
	if !a.IsRepo(ctx) {
		return nil, ErrNotARepo
	}
	cmd := exec.CommandContext(ctx, "git", "-C", a.root, "status", "--porcelain=v1", "-z")
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	return parsePorcelainV1(out), nil
}

// Versions returns the HEAD and current working-tree contents for a path.
// Either may be empty (added file: `original` empty; deleted file: `current` empty).
func (a *API) Versions(ctx context.Context, path string) (original, current string, err error) {
	if !a.IsRepo(ctx) {
		return "", "", ErrNotARepo
	}
	original, _ = a.gitShow(ctx, path)
	currentBytes, err := os.ReadFile(filepath.Join(a.root, path))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return original, "", err
	}
	return original, string(currentBytes), nil
}

// gitShow returns the contents of `path` at HEAD, or empty string if the
// file is new in the working tree.
func (a *API) gitShow(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", a.root, "show", "HEAD:"+path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// "fatal: path '...' exists on disk, but not in 'HEAD'" → treat as empty.
		if strings.Contains(stderr.String(), "exists on disk") ||
			strings.Contains(stderr.String(), "does not exist") ||
			strings.Contains(stderr.String(), "not in 'HEAD'") {
			return "", nil
		}
		return "", fmt.Errorf("git show: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// parsePorcelainV1 reads NUL-terminated `git status --porcelain=v1 -z` output.
//
// Each entry is `XY <space> path [\0 oldpath]\0`. XY are the index/worktree
// status codes (e.g. "M ", " M", "??", "R "). Renames/copies include an
// extra NUL-separated old path.
func parsePorcelainV1(buf []byte) []Change {
	out := []Change{}
	parts := bytes.Split(buf, []byte{0})
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if len(entry) < 3 {
			continue
		}
		x, y := entry[0], entry[1]
		path := string(bytes.TrimLeft(entry[2:], " "))
		var oldPath string
		// Renames carry the old path as the next NUL-separated chunk.
		if x == 'R' || y == 'R' || x == 'C' || y == 'C' {
			if i+1 < len(parts) {
				oldPath = string(parts[i+1])
				i++
			}
		}
		change := Change{Path: path, OldPath: oldPath, Kind: kindFromStatus(x, y)}
		change.Staged = x != ' ' && x != '?' && x != '!'
		out = append(out, change)
	}
	return out
}

func kindFromStatus(x, y byte) ChangeKind {
	switch {
	case x == '?' && y == '?':
		return ChangeUntracked
	case x == 'A' || y == 'A':
		return ChangeAdded
	case x == 'D' || y == 'D':
		return ChangeDeleted
	case x == 'R' || y == 'R':
		return ChangeRenamed
	case x == 'C' || y == 'C':
		return ChangeCopied
	default:
		return ChangeModified
	}
}

// ErrNotARepo is returned when the workspace isn't a git working tree.
var ErrNotARepo = errors.New("workspace is not a git repository")
