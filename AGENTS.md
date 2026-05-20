# Agent notes for coding-anywhere

This repo is a monorepo for **Coding Anywhere**: a Go relay + Go daemon + React/Vite web client for controlling a dev machine through an outbound relay connection.

## Repository shape

- `apps/relay/`: Go relay service. Handles pairing, JWT auth, WebSocket routing, HTTP proxy, Web Push, and SQLite-backed auth/routing state.
- `apps/daemon/`: Go daemon that runs on the dev machine. It loads persisted credentials, connects outbound to the relay, and exposes terminals, chat/Devin, files, git diff, and localhost ports.
- `apps/web/`: React + Vite + Mantine browser UI.
- `packages/protocol/`: TypeScript protocol package. Contains REST pairing client, WebSocket client, zod schemas, and channel payload types.
- `packages/agents/`, `packages/state/`, `packages/tokens/`, `packages/utils/`: shared TS packages.
- `internal/`: Go libraries used by daemon/relay: auth, pairing client, relay client, PTY mux, chat router, file/diff/ports APIs, wire protocol, etc.
- `proto/`: JSON schemas used by codegen.
- `docs/`: architecture/setup docs and service templates.
- `docker/relay/`: relay Dockerfile, compose file, and Caddy config.

## Common commands

Run commands from the repo root unless there is a reason not to.

- Install deps: `make install`
- Full dev stack: `make dev`
- Full build: `make build`
- Full test: `make test`
- JS/TS:
  - Web tests: `pnpm --filter @coding-anywhere/web test`
  - Web typecheck: `pnpm --filter @coding-anywhere/web typecheck`
  - Web build: `pnpm --filter @coding-anywhere/web build`
  - Format selected files: `pnpm exec prettier --write <files>`
- Go:
  - Full Go tests: `go test ./...`
  - Full Go build: `go build ./...`
  - Format Go files: `gofmt -w <files>`
- Whitespace check before finishing: `git diff --check`

## Pairing and auth flow

- Pairing starts with `POST /v1/pair/init`, returns a 6-digit code plus `pairing_token`.
- The other device calls `POST /v1/pair/claim` with the code.
- The initiator polls `GET /v1/pair/status?pairing_token=...` until it receives claimed credentials.
- Relay code:
  - Pair handlers: `apps/relay/internal/server/pair.go`
  - Refresh handler: `apps/relay/internal/server/auth.go`
  - Credential issuance: `apps/relay/internal/server/credentials.go`
  - Routes: `apps/relay/internal/server/routes.go`
  - WS auth/connect: `apps/relay/internal/server/ws.go`
  - SQLite repos/types: `apps/relay/internal/store/`
- Access tokens are JWTs. Refresh tokens are random opaque tokens stored only as SHA-256 hashes in relay SQLite.
- Web credentials are browser-local: `localStorage["coding-anywhere/credentials"]`. Different browsers do not share this state; a new browser must pair again or use a future account/login/import flow.
- Web-side credential code:
  - Storage: `apps/web/src/store/credentials.ts`
  - Connection/refresh scheduling: `apps/web/src/store/connection.ts`
  - Pair screen: `apps/web/src/components/PairScreen.tsx`
- Daemon credentials are stored at `$XDG_DATA_HOME/coding-anywhere/credentials.json` by default, implemented in `apps/daemon/internal/credentials/credentials.go`.
- Daemon pairing CLI is in `apps/daemon/internal/initcmd/initcmd.go`.

## Web app orientation

- Entry: `apps/web/src/App.tsx`
- UI panels:
  - Chat: `apps/web/src/components/ChatPanel.tsx`
  - Terminals: `apps/web/src/components/TerminalsPanel.tsx`
  - Files: `apps/web/src/components/FilesPanel.tsx`
  - Diff: `apps/web/src/components/DiffPanel.tsx`
  - Ports: `apps/web/src/components/PortsPanel.tsx`
  - Notifications: `apps/web/src/components/NotificationsMenu.tsx`
- Zustand stores live under `apps/web/src/store/`.
- WebSocket client implementation is `packages/protocol/src/relay-ws-client.ts`.
- REST pairing client implementation is `packages/protocol/src/pair.ts`.

## Daemon orientation

- CLI entry: `apps/daemon/cmd/main.go`
- The daemon loads credentials, constructs `internal/relayclient.Client`, registers channel handlers, and runs until interrupted.
- Channel routers:
  - Terminals: `internal/terminalrouter`
  - Chat/Devin ACP: `internal/chatrouter`, `internal/agent/devin`, `internal/acp`
  - Files: `internal/filesrouter`, `internal/fileapi`
  - Git diff: `internal/diffrouter`, `internal/diffapi`
  - Ports/proxy: `internal/portsrouter`, `internal/portsapi`

## Relay orientation

- CLI entry: `apps/relay/cmd/main.go`
- Config defaults/env parsing: `apps/relay/internal/server/config.go`
- HTTP middleware/CORS: `apps/relay/internal/server/middleware.go`
- Hub/routing: `apps/relay/internal/server/hub.go`
- HTTP proxy for localhost preview: `apps/relay/internal/server/proxy.go`
- Push notifications: `apps/relay/internal/server/push.go`

## Verification habits

- For frontend changes, prefer at least:
  - `pnpm --filter @coding-anywhere/web test`
  - `pnpm --filter @coding-anywhere/web typecheck`
- For frontend production-risk changes, also run:
  - `pnpm --filter @coding-anywhere/web build`
- For Go changes, run targeted package tests first, then `go test ./...` when scope is broad.
- Keep generated/build output out of git unless explicitly intended.
