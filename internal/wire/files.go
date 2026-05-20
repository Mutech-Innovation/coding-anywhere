package wire

// Methods on the `files` channel let clients browse and read the workspace
// rooted at the daemon's launch directory (overridable via --workspace).
//
// Bytes returned by files.read are base64-encoded because the wire format
// is JSON. Binary files are detected on the daemon and not shipped unless
// the caller explicitly opts in.
const (
	FilesMethodListWorkspace = "list_workspace"
	FilesMethodList          = "list"
	FilesMethodRead          = "read"
	FilesMethodStat          = "stat"
)

// FilesEntryKind mirrors internal/fileapi.EntryKind.
type FilesEntryKind string

const (
	FilesEntryFile      FilesEntryKind = "file"
	FilesEntryDirectory FilesEntryKind = "directory"
	FilesEntrySymlink   FilesEntryKind = "symlink"
	FilesEntryOther     FilesEntryKind = "other"
)

// FilesEntry is one item in a directory listing.
type FilesEntry struct {
	Name    string         `json:"name"`
	Path    string         `json:"path"`
	Kind    FilesEntryKind `json:"kind"`
	Size    int64          `json:"size"`
	ModTime int64          `json:"mod_time"`
	Target  string         `json:"target,omitempty"`
}

// --- Workspace ---

// FilesWorkspaceInfo describes a single workspace root.
type FilesWorkspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
}

// FilesListWorkspaceResponse returns the workspaces the daemon serves. For
// MVP this is a singleton.
type FilesListWorkspaceResponse struct {
	Workspaces []FilesWorkspaceInfo `json:"workspaces"`
}

// --- list ---

type FilesListRequest struct {
	WorkspaceID   string `json:"workspace_id,omitempty"`
	Path          string `json:"path"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	MaxEntries    int    `json:"max_entries,omitempty"`
}

type FilesListResponse struct {
	WorkspaceID string       `json:"workspace_id"`
	Path        string       `json:"path"`
	Entries     []FilesEntry `json:"entries"`
}

// --- read ---

type FilesReadRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Path        string `json:"path"`
	Offset      int64  `json:"offset,omitempty"`
	Length      int64  `json:"length,omitempty"`
	MaxBytes    int64  `json:"max_bytes,omitempty"`
	AllowBinary bool   `json:"allow_binary,omitempty"`
}

type FilesReadResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Truncated   bool   `json:"truncated,omitempty"`
	IsBinary    bool   `json:"is_binary,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Data        string `json:"data"` // base64 of bytes (empty when withholding binary)
}

// --- stat ---

type FilesStatRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Path        string `json:"path"`
}

type FilesStatResponse struct {
	WorkspaceID string     `json:"workspace_id"`
	Entry       FilesEntry `json:"entry"`
}
