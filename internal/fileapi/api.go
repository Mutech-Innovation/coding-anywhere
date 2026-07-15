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
	"path"
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
	MaxEntries    int // 0 uses the bounded default
}

// List enumerates direct children of `relPath` (relative to root). The
// listing is non-recursive; clients drill in by repeating List on a child.
func (a *API) List(relPath string, opts ListOptions) ([]Entry, error) {
	const maxEntries = 1_000
	if opts.MaxEntries <= 0 || opts.MaxEntries > maxEntries {
		opts.MaxEntries = maxEntries
	}
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
	// MaxBytes caps how many bytes we read. Zero defaults to 1 MiB; larger
	// requests are clamped to 4 MiB.
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
	} else if maxBytes > 4<<20 {
		maxBytes = 4 << 20
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

// WriteOptions tweaks Write behaviour.
type WriteOptions struct {
	// Offset is the byte position to write `data` at. Callers stream large
	// uploads as a sequence of Write calls with increasing offsets to stay
	// under the WS transport's per-message cap.
	Offset int64
	// Truncate clears any existing file before writing. Set it on the first
	// chunk of an upload (offset 0) so a re-upload replaces, not appends.
	Truncate bool
}

// Write creates or updates a regular file at relPath, writing `data` at
// opts.Offset. The parent directory must already exist. Returns the file's
// size after the write.
func (a *API) Write(relPath string, data []byte, opts WriteOptions) (int64, error) {
	abs, err := a.resolveNew(relPath)
	if err != nil {
		return 0, err
	}
	if info, err := os.Lstat(abs); err == nil && info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", relPath)
	}
	flag := os.O_CREATE | os.O_WRONLY
	if opts.Truncate {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(abs, flag, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteAt(data, opts.Offset); err != nil {
		return 0, err
	}
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Remove deletes the file or directory at relPath. Directories are removed
// recursively. The workspace root itself can never be removed.
func (a *API) Remove(relPath string) error {
	abs, err := a.resolveNew(relPath)
	if err != nil {
		return err
	}
	// resolveNew refuses the root, but guard against a symlinked parent
	// resolving back onto it anyway.
	if abs == a.root {
		return ErrEscapesRoot
	}
	if _, err := os.Lstat(abs); err != nil {
		return err // surface a normal not-found
	}
	return os.RemoveAll(abs)
}

// Move renames/moves the entry at fromRel to toRel. Both stay inside the
// root. It refuses to overwrite an existing destination so a rename can't
// silently clobber another file.
func (a *API) Move(fromRel, toRel string) error {
	from, err := a.resolveNew(fromRel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(from); err != nil {
		return err
	}
	to, err := a.resolveNew(toRel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("destination already exists: %s", toRel)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(from, to)
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

// resolveNew maps relPath to an absolute path for a mutating operation
// (write/delete/move). Unlike resolve it does NOT follow a symlink at the
// final component — so deleting/moving a symlink acts on the link itself —
// and it never targets the root. It still resolves (and containment-checks)
// the PARENT directory through symlinks, so a symlinked parent can't be used
// to escape the root.
func (a *API) resolveNew(relPath string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if clean == "." || clean == "" {
		return "", ErrEscapesRoot // never target the workspace root itself
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", ErrEscapesRoot
	}
	base := path.Base(clean)
	if base == "." || base == ".." || base == "/" || base == "" {
		return "", ErrEscapesRoot
	}
	// Resolve the parent through symlinks and confirm it's inside the root.
	parent, err := a.resolve(path.Dir(clean))
	if err != nil {
		return "", err
	}
	abs := filepath.Join(parent, base)
	rel, err := filepath.Rel(a.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrEscapesRoot
	}
	return abs, nil
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
