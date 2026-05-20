// Package version exposes build-time version information for daemon and relay.
//
// The values are populated by the linker at build time:
//
//	go build -ldflags "-X github.com/Mutech-Innovation/coding-anywhere/internal/version.Version=v0.1.0 \
//	                   -X github.com/Mutech-Innovation/coding-anywhere/internal/version.Commit=$(git rev-parse --short HEAD)"
package version

import "fmt"

var (
	// Version is the semantic version (e.g. "v0.1.0"). Defaults to "dev" for local builds.
	Version = "dev"
	// Commit is the short git commit hash. Defaults to "unknown".
	Commit = "unknown"
	// BuildDate is an RFC3339 timestamp. Defaults to "unknown".
	BuildDate = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}
