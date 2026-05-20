# proto/

JSON Schemas that are the **single source of truth** for messages on the
WebSocket wire. The schemas here are consumed by:

- `tools/codegen/` → emits TypeScript types/zod schemas into
  `packages/protocol/src/generated/` and Go structs into
  `internal/wire/generated/`.

Files in this directory should be hand-written. Anything in `**/generated/`
is overwritten on `pnpm codegen` and must not be edited manually.

## Conventions

- One file per channel: `meta.json`, `sessions.json`, `terminal.json`, …
- All schemas are `draft-2020-12`.
- Each channel file declares **request methods**, **event types**, and
  **shared types** under top-level `$defs`.
- Identifiers are `snake_case` on the wire; codegen handles the casing
  conversion (`camelCase` for TS, `PascalCase` for Go).
