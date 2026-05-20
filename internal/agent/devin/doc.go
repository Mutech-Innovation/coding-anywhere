// Package devin implements the Agent / Session interfaces against
// Devin for Terminal via the Agent Client Protocol (ACP).
//
// The adapter spawns `devin acp` as a subprocess, speaks JSON-RPC over its
// stdio, and translates ACP events into the unified internal Event stream.
// Slash commands or other features not exposed by ACP are handled via a
// sidecar `devin -p` invocation when needed.
package devin
