package wire

const (
	DiffMethodListChanges = "list_changes"
	DiffMethodVersions    = "versions"
)

// DiffChangeKind enumerates the per-file change types.
type DiffChangeKind string

const (
	DiffChangeAdded     DiffChangeKind = "added"
	DiffChangeModified  DiffChangeKind = "modified"
	DiffChangeDeleted   DiffChangeKind = "deleted"
	DiffChangeRenamed   DiffChangeKind = "renamed"
	DiffChangeCopied    DiffChangeKind = "copied"
	DiffChangeUntracked DiffChangeKind = "untracked"
)

// DiffChange is one entry in the workspace's change list.
type DiffChange struct {
	Path    string         `json:"path"`
	OldPath string         `json:"old_path,omitempty"`
	Kind    DiffChangeKind `json:"kind"`
	Staged  bool           `json:"staged,omitempty"`
}

// --- list_changes ---

type DiffListChangesResponse struct {
	Changes []DiffChange `json:"changes"`
	IsRepo  bool         `json:"is_repo"`
}

// --- versions ---

type DiffVersionsRequest struct {
	Path string `json:"path"`
}

type DiffVersionsResponse struct {
	Path     string `json:"path"`
	Original string `json:"original"`
	Current  string `json:"current"`
}
