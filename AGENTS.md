# Agent notes for coding-anywhere

This repo is a monorepo for **Coding Anywhere**: a Go relay + Go daemon +
React/Vite web client for controlling a dev machine through an outbound relay
connection. The dev machine opens no inbound ports — both the daemon and the
browser dial **outbound** to the relay over WSS.

```
┌───────────────┐      ┌──────────────────┐      ┌──────────────────┐
│  Client (web) │<─wss─│  Relay (VPS)     │<─wss─│  Dev machine     │
│  React + Vite │      │  Go: pair/JWT/   │      │  daemon (Go):    │
│               │      │  route/proxy/push│      │  PTY, Devin ACP, │
│               │      │  + SQLite state  │      │  files, diff,... │
└───────────────┘      └──────────────────┘      └──────────────────┘
```

See `docs/architecture.md` for the full design and `docs/setup.md` for an
end-to-end setup walkthrough.

## Repository layout

```
.
├── apps/
│   ├── relay/                 # Go relay service (VPS-side broker)
│   │   ├── cmd/main.go        #   entry point + flag/env wiring
│   │   └── internal/server/   #   HTTP/WS server, hub, pairing, proxy, push
│   ├── daemon/                # Go daemon (runs on each dev machine)
│   │   ├── cmd/main.go        #   entry point + CLI flags
│   │   └── internal/          #   credentials, init/pair CLI
│   ├── web/                   # React + Vite + Mantine browser UI
│   │   └── src/               #   App.tsx, components/, store/ (zustand), test/
│   └── mobile/                # v3 React Native placeholder (currently empty)
├── packages/                  # Shared TypeScript packages (pnpm workspace)
│   ├── protocol/              #   REST pair client, WS client, zod schemas
│   ├── agents/                #   agent metadata/types
│   ├── state/                 #   shared state helpers (web + future mobile)
│   ├── tokens/                #   design tokens
│   └── utils/                 #   misc shared utils
├── internal/                  # Go libraries shared by daemon + relay
│   ├── auth/                  #   JWT signer/verifier, device kinds, claims
│   ├── wire/                  #   envelope types, channel enum, generated/
│   ├── relayclient/           #   daemon's WS client to the relay (+ reconnect)
│   ├── relayproto/            #   shared relay protocol doc/constants
│   ├── pairingclient/         #   REST pairing client used by the daemon
│   ├── ptymux/                #   PTY multiplexer for terminals
│   ├── terminalrouter/        #   terminal channel handler (daemon side)
│   ├── chatrouter/            #   chat channel handler ↔ ACP
│   ├── agent/devin/           #   Devin ACP subprocess adapter
│   ├── acp/                   #   Agent Client Protocol (JSON-RPC) framing
│   ├── fileapi/ filesrouter/  #   files API + files channel handler
│   ├── diffapi/ diffrouter/   #   git diff API + diff channel handler
│   ├── portsapi/ portsrouter/ #   localhost ports API + ports channel handler
│   └── version/               #   version string (ldflags-injected)
├── proto/                     # JSON Schemas: single source of truth for wire types
├── tools/codegen/             # proto/*.json → generated TS + Go (pnpm codegen)
├── docker/relay/              # relay Dockerfile, compose.yml, Caddyfile, .env.example
├── docs/                      # architecture.md, setup.md, service templates
├── scripts/                   # check-devin-acp.py, democlient/, smoke.sh
├── bin/                       # built Go binaries (gitignored)
├── Makefile                   # make install/dev/build/test/lint/fmt/clean
├── turbo.json                 # turbo task graph for the JS side
├── go.mod                     # module github.com/Mutech-Innovation/coding-anywhere
└── pnpm-workspace.yaml        # workspaces: apps/*, packages/*, tools/*
```

Note: `internal/files/` and `internal/ports/` are empty placeholders; the real
code lives in `internal/fileapi` / `internal/portsapi` (+ the matching routers).

## Tech stack & toolchain

- **Go 1.25** (single module at repo root, `go build ./...`).
- **Node >=22**, **pnpm 10.9** (pinned via `packageManager`), **turbo** for the
  JS task graph.
- Web: **React + Vite + Mantine**, **zustand** stores, **CodeMirror** (file
  viewer), **xterm.js** (terminals).
- Relay persistence: **SQLite** (auth/routing/push state only — never chat
  content).
- Lint/format: **prettier** + **golangci-lint v2** + `gofmt` + `go vet`.

## Common commands

Run commands from the repo root unless there is a reason not to.

- Install deps: `make install` (= `pnpm install` + `go mod tidy`)
- Full dev stack: `make dev` (= `turbo run dev --parallel`: web + daemon + relay)
- Full build: `make build`
- Full test: `make test` (= `pnpm test` + `go test ./...`)
- Lint: `make lint` (= `pnpm lint` + `go vet ./...`); Go lint: `golangci-lint run ./...`
- Format everything: `make fmt`
- Regenerate wire types: `pnpm codegen`
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

## Code generation (wire protocol)

`proto/*.json` are hand-written JSON Schemas (draft-2020-12) that are the
**single source of truth** for messages on the WebSocket wire.

- `pnpm codegen` runs `tools/codegen/src/index.ts`, which reads `proto/` and is
  intended to emit TypeScript into `packages/protocol/src/generated/` and Go
  structs into `internal/wire/generated/`. NOTE: the generator is still a
  placeholder today (it scans `proto/` and prints what it would emit); the
  `generated/` dirs are not produced yet. Real generation lands later.
- Identifiers are `snake_case` on the wire; codegen converts to `camelCase`
  (TS) and `PascalCase` (Go).
- Never hand-edit anything under `**/generated/` — it is overwritten on
  `pnpm codegen`. Edit the schema in `proto/` instead.
- See `proto/README.md` for the channel-file conventions.

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
- Access tokens are JWTs (HS256, 1h for clients, 30d for daemons). Refresh
  tokens are random opaque tokens stored only as SHA-256 hashes in relay SQLite.
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
- CLI flags:
  - `-relay-url` — override the relay URL stored in credentials
  - `-workspace` — workspace root for the files API (default `$HOME`)
  - `-credentials` — path to the credentials file
  - `-log-level` — `debug|info|warn|error`
  - `-version`
  - `init` subcommand — interactive pairing (prints a 6-digit code)
- Channel routers:
  - Terminals: `internal/terminalrouter`
  - Chat/Devin ACP: `internal/chatrouter`, `internal/agent/devin`, `internal/acp`
  - Files: `internal/filesrouter`, `internal/fileapi`
  - Git diff: `internal/diffrouter`, `internal/diffapi`
  - Ports/proxy: `internal/portsrouter`, `internal/portsapi`

## Relay orientation

- CLI entry: `apps/relay/cmd/main.go` (flags: `-addr`, `-log-level`, `-version`)
- Config defaults/env parsing: `apps/relay/internal/server/config.go`
- HTTP middleware/CORS: `apps/relay/internal/server/middleware.go`
- Hub/routing: `apps/relay/internal/server/hub.go`
- HTTP proxy for localhost preview: `apps/relay/internal/server/proxy.go`
- Push notifications: `apps/relay/internal/server/push.go`
- The relay serves **plain HTTP** on `CA_LISTEN_ADDR` (default `:8443`); it does
  no TLS itself. TLS is terminated by the reverse proxy in front of it.
- Config env vars (`apps/relay/internal/server/config.go`):
  - `CA_RELAY_JWT_SECRET` — **required**, >=32 bytes (e.g. `openssl rand -base64 48`).
    If unset, the relay falls back to an insecure dev default and logs a loud
    warning — never use that in production.
  - `CA_LISTEN_ADDR` — bind address (default `:8443`)
  - `CA_RELAY_DATA_DIR` — SQLite/state dir (default `./relay-data`, `/data` in Docker)
  - `CA_RELAY_JWT_ISSUER` — JWT `iss` claim (default `coding-anywhere/relay`)

## Deployment

The relay is meant to be deployed **standalone** on a small public VPS; the
daemon and browser only need its public HTTPS URL.

### Option A — bundled compose (Caddy terminates TLS)

`docker/relay/` ships a turnkey stack (relay + Caddy with automatic Let's
Encrypt). See `docker/relay/compose.yml` and `docs/setup.md`:

```bash
cd docker/relay
cp .env.example .env          # set CA_DOMAIN + CA_RELAY_JWT_SECRET
docker compose up -d --build
```

Smoke test: `curl https://<domain>/healthz` and `/v1/version` should both
return JSON.

### Option B — behind an existing reverse proxy (Traefik / Nginx)

Since the relay is plain HTTP on `:8443`, drop the bundled Caddy and let your
proxy terminate TLS and forward to `relay:8443`. WebSocket needs no special
config (Traefik passes `Upgrade` through automatically; for Nginx set the usual
`Upgrade`/`Connection` headers). Minimal Traefik label set on the relay
container:

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.ca-relay.rule=Host(`relay.example.com`)"
  - "traefik.http.routers.ca-relay.entrypoints=websecure"
  - "traefik.http.routers.ca-relay.tls.certresolver=le"
  - "traefik.http.services.ca-relay.loadbalancer.server.port=8443"
```

### Endpoints to verify a live relay

- `GET /healthz` → `{"ok":true,...}`
- `GET /v1/version` → `{"issuer":...,"protocol_version":1}`
- `GET /v1/ws/connect` with no/invalid token → relay's own `401 unauthorized`
  (proves the WS route + upgrade headers reach the relay, not a proxy 404/502).
  A real `101 Switching Protocols` only happens with a valid paired JWT.

### Operational notes

- **Back up the relay data volume** (`relay-data` → `/data`): it holds accounts,
  pairings, refresh-token hashes, and Web Push subscriptions. Losing it forces
  every device to re-pair.
- **Rotating `CA_RELAY_JWT_SECRET` invalidates all issued access tokens** —
  devices must re-pair.
- Never commit `.env` or the secret (`.env` is gitignored).

## CI

`.github/workflows/ci.yml` runs on push to `main` and on PRs:

- **JS / TS**: `pnpm typecheck`, `pnpm format:check`, `pnpm test`, `pnpm build`.
- **Go**: `go vet ./...`, `go test ./...`, `go build ./...`.
- **golangci-lint** (v2.12.2).

Run the relevant subset locally before pushing to keep CI green.

## Verification habits

- For frontend changes, prefer at least:
  - `pnpm --filter @coding-anywhere/web test`
  - `pnpm --filter @coding-anywhere/web typecheck`
- For frontend production-risk changes, also run:
  - `pnpm --filter @coding-anywhere/web build`
- For Go changes, run targeted package tests first, then `go test ./...` when scope is broad.
- If you touch `proto/`, run `pnpm codegen` and commit the regenerated output.
- Keep generated/build output out of git unless explicitly intended.

## Devin CLI version pinning (ACP regression workaround)

The daemon spawns `devin acp` as a subprocess (see `internal/agent/devin/adapter.go`). The `acp` subcommand has been less stable than the interactive REPL — at least one shipped version (`2026.5.26-0`) regressed `session/prompt` for Pro accounts so every prompt comes back as JSON-RPC error `-32013` *"Permission denied: ... an internal error occurred (trace ID: ...)"*. The interactive `devin --print` path was unaffected, so plain `devin auth status` looks fine even when chat is broken.

Symptoms in `make dev` chat:
- New session can be created (status → `idle`).
- Sending the first message fails immediately with `prompt_failed: rpc error -32013: Permission denied: ...`.
- The daemon's stderr (from `pumpStderr`, when `--log-level=debug`) shows `WARN chisel_core::translator: ACP: agent error (Internal): Permission denied` and a giveaway `WARN agent_ext::raindrop: 403: Telemetry proxy is only available for free-tier users` even on Pro accounts.

### Detect

`scripts/check-devin-acp.py` drives a `devin acp` subprocess through `initialize` → `session/new` → `session/prompt` and exits 0 on PASS / 2 on regression. Use it before adopting any new `devin` version:

```bash
# Test whatever `devin` is on PATH:
scripts/check-devin-acp.py
# Test a specific binary (use this against newly downloaded versions):
scripts/check-devin-acp.py --devin ~/.local/share/devin/cli/_versions/<ver>/bin/devin
```

### Pin to a working version

The `devin` installer keeps every downloaded version under `~/.local/share/devin/cli/_versions/<ver>` and points `~/.local/share/devin/cli/_versions/current` at the active one. To pin a specific version:

1. Disable background updates so `devin` doesn't quietly re-promote a broken release. In `~/.config/devin/config.json` (`%APPDATA%\devin\config.json` on Windows) set `"auto_update": false`.
2. Repoint the symlink: `ln -sfn <ver> ~/.local/share/devin/cli/_versions/current` (e.g. `2026.5.6-12` for the regression above).
3. Restart `make dev` so the daemon respawns `devin acp` against the new binary. The previous `devin acp` child keeps running until the daemon exits.

### Re-evaluate later

When a new `devin` release comes out, don't just flip `auto_update` back on. Download the candidate (e.g. via `devin update --force` to populate `_versions/`, or by grabbing the installer manually), run `scripts/check-devin-acp.py --devin <candidate>` against it, and only repoint `current` after PASS.
