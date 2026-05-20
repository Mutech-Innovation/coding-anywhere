// Public exports for @coding-anywhere/protocol.
//
// This package is the single source of truth on the TypeScript side for the
// WebSocket envelope format and channel payloads. Schemas are kept in sync
// with internal/wire on the Go side via codegen from proto/*.json.

export * from './chat.js';
export * from './diff.js';
export * from './envelope.js';
export * from './files.js';
export * from './pair.js';
export * from './relay-ws-client.js';
export * from './terminal.js';
