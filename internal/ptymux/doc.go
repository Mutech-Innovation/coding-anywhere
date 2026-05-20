// Package ptymux provides a multiplexer over PTY-backed processes used to
// power both the standalone terminal feature and the agent transport.
//
// Each session has a fixed-size ring buffer for scrollback and supports
// attach/detach, resize, and replay-on-attach semantics so that clients can
// reconnect without losing recent output.
package ptymux
