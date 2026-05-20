// Package fileapi exposes a workspace-scoped file API for the daemon.
//
// All operations are constrained to a workspace root provided at
// construction time. Symlinks that escape the root are rejected. Binary
// files are detected so we can refuse to ship raw bytes through a JSON
// transport (callers should request a chunked range instead).
package fileapi

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// API is the workspace-scoped file API. Construct with New().
type API struct {
	root string // absolute, evaluated path
}

// New returns an API rooted at `root`. The path is evaluated to an absolute
// canonical form; symlinks pointing outside `root` will be rejected at
// access time.
func New(root string) (*API, error) {
	if root == "" {
		return nil, errors.New("root required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If the directory doesn't exist yet, use the absolute path. Read
		// operations will then fail with a normal "not found".
		if errors.Is(err, fs.ErrNotExist) {
			resolved = abs
		} else {
			return nil, fmt.Errorf("eval root: %w", err)
		}
	}
	info, err := os.Stat(resolved)
	if err == nil && !info.IsDir() {
		return nil, fmt.Errorf("root must be a directory: %s", resolved)
	}
	return &API{root: resolved}, nil
}

// Root returns the absolute root path.
func (a *API) Root() string { return a.root }

// EntryKind classifies a directory entry.
type EntryKind string

const (
	EntryFile      EntryKind = "file"
	EntryDirectory EntryKind = "directory"
	EntrySymlink   EntryKind = "symlink"
	EntryOther     EntryKind = "other"
)

// Entry describes one item returned by List.
type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // relative to root, "/" separators
	Kind    EntryKind `json:"kind"`
	Size    int64     `json:"size"`
	ModTime int64     `json:"mod_time"` // unix milliseconds
	// For symlinks: target as the OS reports it.
	Target string `json:"target,omitempty"`
}

// ListOptions tweaks List behaviour.
type ListOptions struct {
	IncludeHidden bool
	MaxEntries    int // 0 = no limit
}

// List enumerates direct children of `relPath` (relative to root). The
// listing is non-recursive; clients drill in by repeating List on a child.
func (a *API) List(relPath string, opts ListOptions) ([]Entry, error) {
	abs, err := a.resolve(relPath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if !opts.IncludeHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		entry := Entry{
			Name:    e.Name(),
			Path:    joinSlash(relPath, e.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.Kind = EntrySymlink
			if target, err := os.Readlink(filepath.Join(abs, e.Name())); err == nil {
				entry.Target = target
			}
		case info.IsDir():
			entry.Kind = EntryDirectory
		case info.Mode().IsRegular():
			entry.Kind = EntryFile
		default:
			entry.Kind = EntryOther
		}
		out = append(out, entry)
		if opts.MaxEntries > 0 && len(out) >= opts.MaxEntries {
			break
		}
	}
	return out, nil
}

// ReadResult is what Read returns.
type ReadResult struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Truncated   bool   `json:"truncated,omitempty"`
	IsBinary    bool   `json:"is_binary,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Bytes       []byte `json:"-"`
}

// ReadOptions tweaks Read behaviour.
type ReadOptions struct {
	// MaxBytes caps how many bytes we read. Zero means a 1 MiB default.
	MaxBytes int64
	// Offset / Length read a slice of the file (Length == 0 → to EOF).
	Offset int64
	Length int64
	// AllowBinary returns binary contents without forcing the caller to
	// opt in. Defaults to false: binary files return only metadata + the
	// is_binary flag.
	AllowBinary bool
}

// Read returns the file's bytes, with binary-file safety and a default size
// cap so we don't blow up the WebSocket transport.
func (a *API) Read(relPath string, opts ReadOptions) (*ReadResult, error) {
	abs, err := a.resolve(relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", relPath)
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}

	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if opts.Offset > 0 {
		if _, err := f.Seek(opts.Offset, 0); err != nil {
			return nil, err
		}
	}

	wantRead := info.Size() - opts.Offset
	if wantRead < 0 {
		wantRead = 0
	}
	if opts.Length > 0 && opts.Length < wantRead {
		wantRead = opts.Length
	}
	truncated := false
	if wantRead > maxBytes {
		wantRead = maxBytes
		truncated = true
	}
	buf := make([]byte, wantRead)
	n, err := readAll(f, buf)
	if err != nil {
		return nil, err
	}
	buf = buf[:n]

	contentType := http.DetectContentType(buf)
	isBinary := classifyBinary(buf, contentType)
	res := &ReadResult{
		Path:        relPath,
		Size:        info.Size(),
		Truncated:   truncated,
		IsBinary:    isBinary,
		ContentType: contentType,
	}
	if !isBinary || opts.AllowBinary {
		res.Bytes = buf
	}
	return res, nil
}

// Stat returns metadata about a path.
func (a *API) Stat(relPath string) (Entry, error) {
	abs, err := a.resolve(relPath)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{
		Name:    info.Name(),
		Path:    relPath,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixMilli(),
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		entry.Kind = EntrySymlink
		if target, err := os.Readlink(abs); err == nil {
			entry.Target = target
		}
	case info.IsDir():
		entry.Kind = EntryDirectory
	case info.Mode().IsRegular():
		entry.Kind = EntryFile
	default:
		entry.Kind = EntryOther
	}
	return entry, nil
}

// resolve maps relPath onto the workspace root and verifies the result is
// inside the root (i.e. does not escape via ".." or symlinks).
func (a *API) resolve(relPath string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if clean == "." {
		clean = ""
	}
	// Reject explicit upward traversal before we even hit the filesystem.
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", ErrEscapesRoot
	}
	abs := filepath.Join(a.root, clean)
	resolved, err := filepath.EvalSymlinks(abs)
	if errors.Is(err, fs.ErrNotExist) {
		// Allow listing/reading not-yet-created paths to bubble up a normal
		// not-found error instead of a security error.
		return abs, nil
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(a.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "..") {
		return "", ErrEscapesRoot
	}
	return resolved, nil
}

// joinSlash joins a relative parent and a child name with a forward slash.
func joinSlash(parent, name string) string {
	parent = strings.Trim(filepath.ToSlash(parent), "/")
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func readAll(f *os.File, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := f.Read(buf[total:])
		total += n
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return total, err
		}
	}
	return total, nil
}

// classifyBinary reports whether `buf` looks like a binary file. We use a
// combination of the http.DetectContentType output (Content-Type) and a
// quick UTF-8 validity check on the first ~8 KiB. Empty files are NOT
// considered binary.
func classifyBinary(buf []byte, contentType string) bool {
	if len(buf) == 0 {
		return false
	}
	if strings.HasPrefix(contentType, "text/") {
		return false
	}
	switch contentType {
	case "application/json", "application/xml", "application/javascript",
		"application/x-yaml", "application/x-sh", "application/x-shellscript":
		return false
	}
	probe := buf
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	if !utf8.Valid(probe) {
		return true
	}
	// Reject if there are NUL bytes — almost always binary in real life.
	for _, b := range probe {
		if b == 0x00 {
			return true
		}
	}
	return false
}

// ErrEscapesRoot is returned when a path resolves outside the workspace root.
var ErrEscapesRoot = errors.New("path escapes workspace root")
