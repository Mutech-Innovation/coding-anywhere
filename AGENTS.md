# Agent notes for coding-anywhere

This repo is a monorepo for **Coding Anywhere**: a Go relay + Go daemon +
React/Vite web client for controlling a dev machine through an outbound relay
connection. The dev machine opens no inbound ports — both the daemon and the
browser dial **outbound** to the relay over WSS.

```
┌───────────────┐      ┌──────────────────┐      ┌──────────────────┐
│  Client (web) │<─wss─│  Relay (VPS)     │<─wss─│  Dev machine     │
│  React + Vite │      │  Go: pair/JWT/   │      │  daemon (Go):    │
│               │      │  route/proxy/push│      │  PTY, agent ACP, │
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
│   ├── chatrouter/            #   chat channel handler ↔ ACP (multi-agent routing)
│   ├── agent/acpagent/        #   generic ACP subprocess adapter (Devin, cursor-agent)
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

## Guest "ask"-mode share links

A short-lived, read-only public link that lets someone chat with one of the
account's agents (Devin or cursor-agent) in **ask** mode without pairing.
Security is enforced at the relay (who can do/see what) and the daemon (the
agent stays read-only); the web UI hiding controls is cosmetic only.

- **Token**: a new device kind `auth.DeviceGuest` (`internal/auth/jwt.go`). Guest
  JWTs have an `exp` and **no refresh token** — they just expire. The JWT
  `sub`/device id is the share id.
- **Share record**: `guest_shares` table (`migrations/0004_guest_shares.sql` +
  `0005_guest_share_cwd.sql` + `0006_guest_share_model.sql` +
  `0007_guest_share_agent.sql`, repo methods in `store/repos.go`) holds
  account, expiry, `revoked`, the caller-chosen `prompt_limit` /
  `max_sessions`, and the owner-chosen `cwd` / `model` / `agent_id`.
- **Issue / revoke** (owner-authenticated): `POST /v1/share/ask` and
  `DELETE /v1/share/{id}` in `apps/relay/internal/server/share.go`. TTL, prompt
  limit, session count, cwd, model, and agent are all fixed at issuance.
  Config bounds live in `config.go` (`CA_GUEST_MAX_TTL`, `GuestDefaultTTL`,
  optional `CA_GUEST_WEB_BASE_URL`).
- **Working directory + model + agent**: guests have no directory/model/agent
  picker, but the daemon's chat agents refuse to run without an explicit
  project cwd. The owner picks the cwd (required), optionally a model, and the
  agent (devin / cursor-cli) when minting the share; the relay **injects** all
  three into the guest's `create_session` (`injectCreateFields` in
  `guest.go`), so the guest can neither omit nor tamper with them. The daemon
  then applies the locked model right after creating a guest session via the
  agent-appropriate mechanism (`chatrouter.applySessionModel`:
  `set_config_option` for Devin, `session/set_model` for cursor-agent). An
  empty cwd (legacy shares) fails with `cwd required` until re-issued; an
  empty model falls back to the agent's default; an empty agent falls back to
  the daemon default (devin).
- **Relay enforcement** (`apps/relay/internal/server/guest.go`, wired in
  `ws.go` + `hub.go`):
  - chat-only allowlist (`create_session`/`prompt`/`cancel`/`get_history`);
    rejects `set_mode`, `resolve_permission`, all non-chat channels.
  - session isolation: a guest only sees/sends to sessions it created; the hub
    filters daemon→guest fan-out by owned session id.
  - concurrent viewers: the hub keys guests by **connection id** (not the
    shared share-id/device-id), so multiple people can open the same link at
    once without evicting each other. `Hub.CloseGuest` (revoke) drops every
    connection whose device id matches the share.
  - per-link prompt + session caps; auto-disconnect at token expiry; live
    disconnect on revoke (`Hub.CloseGuest`).
  - rejected guest connects (expired/revoked/unknown share, expired token) are
    **accepted then closed** with application close code `4401` + a short
    reason — browsers can't read the HTTP status of a failed WS handshake, so
    this is how the guest page learns why (it gives up and shows the reason
    instead of reconnect-looping; it also pre-checks the token's `exp`
    client-side). Pinned by `TestGuestRejectedWithCloseReason`.
  - stamps `wire.Envelope.Origin = "guest"` on forwarded traffic and strips any
    client-set Origin on ingress (trust marker for the daemon).
- **Daemon enforcement** (`internal/chatrouter/router.go`): a session whose
  create request carries `Origin=guest` is forced into ask mode at creation
  (`askModeID` + `SetMode`) and **auto-denies every permission request**
  (`handleAgentPermissionRequest` returns a `cancelled` outcome).
- **Web**: standalone page at `/guest?token=...&relay=...`
  (`apps/web/src/GuestApp.tsx` + `components/GuestChat.tsx`), routed in
  `main.tsx`, reusing the `useChat` store with a stripped chat-only UI. The
  owner mints links from the device menu (`components/ShareAskMenu.tsx` →
  `connection.ts` `createAskShare` → `PairingClient.createAskShare`); the share
  modal has an agent picker (from the daemon's `chat.agents`), reuses
  `ChatPanel`'s exported `DirectoryPicker` for the cwd, and a `Select` for the
  model (choices pulled from an open session of the **same agent** — model ids
  differ per agent). The guest page also reuses `ChatPanel`'s exported
  `MessageBubble` / `ToolCallCard` so it renders markdown/tool cards like the
  main chat, and names the session's agent in its copy.
- **Tests**: `apps/relay/internal/server/guest_internal_test.go` (gating logic),
  `guest_e2e_test.go` (force-ask + gating + isolation + permission auto-deny
  end to end), `store/store_test.go` (`TestGuestShareCRUD`).
- **Deploying the guest page behind a reverse proxy**: `/guest` is a client-side
  route, so the static host needs SPA fallback (serve `index.html` for unknown
  paths). Vite dev does this automatically.

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
  - Chat/agents (ACP): `internal/chatrouter`, `internal/agent/acpagent`, `internal/acp`
  - Files: `internal/filesrouter`, `internal/fileapi`
  - Git diff: `internal/diffrouter`, `internal/diffapi`
  - Ports/proxy: `internal/portsrouter`, `internal/portsapi`

## Chat agents (Devin + cursor-agent)

The chat channel is multi-agent. `internal/agent/acpagent` is one generic ACP
stdio adapter; `internal/chatrouter` keeps a `Builders` map (agent id →
builder) and lazily spawns one adapter per agent on first `create_session`:

- `devin` → `devin acp` (`DefaultDevinBuilder`)
- `cursor-cli` → `cursor-agent acp` (`DefaultCursorBuilder`)

`chat.agents` reports availability by checking the CLI binary on PATH (only
under the default wiring; injected test builders are trusted as-is).
Cursor-specific quirks, all handled inside the daemon:

- **Auth**: cursor's ACP rejects `session/new` until an
  `authenticate(cursor_login)` call succeeds; the adapter does this during
  Start when the agent advertises the method. Credentials come from
  `cursor-agent login` on the daemon machine (run it once, interactively).
  A stale login can make `authenticate` hang — the adapter bounds the call
  (30s). `authenticate` also needs the **network** even when credentials are
  fine: after the local login check it initializes "shared services"
  (statsig/server-config fetches against `api2.cursor.sh`), and if those
  fail it answers a generic `-32603 Internal error` ~21s in, with the real
  cause (e.g. `[unavailable] connect …`) only in the error's `data` field —
  which `acp.RPCError.Error()` therefore includes. Failures are wrapped in
  `acpagent.AuthFailedError` so `buildAdapterForSession` can stop instead of
  retrying model-id spellings that can't help. Note cursor-agent (Node) does
  NOT use the Windows system proxy; it honors `HTTPS_PROXY`/`HTTP_PROXY` env
  vars inherited from the daemon. Debug logs: `$TMPDIR/cursor-agent-logs/`.
- **Models**: cursor advertises models via the ACP session-model extension
  (`models` on `session/new`), and the chatrouter synthesises a
  `config_option_update` (option id `model`) from it so the web model picker
  works unchanged. BUT the generation model of a running cursor process is
  **fixed at spawn** (`--model` flag): `session/set_model` only validates and
  persists a preference for future processes, and `model` fields on
  session/new are ignored (verified by live probing). The router therefore
  runs **one cursor subprocess per chat session** (`perSessionProcess`),
  pinning `Options.Model → --model` at spawn (used by the guest share's
  locked model), and a `chat.set_config_option("model")` write restarts the
  session's subprocess pinned to the new model and resumes the conversation
  via `session/load` (`restartSessionWithModel`) — the old process must be
  killed first (cursor refuses to load a chat another live process holds)
  and the load's history replay is suppressed (`session.suppressUpdates`)
  so clients don't see a duplicated transcript. `session/load`'s
  `mcpServers` must be `[]`, never `null` (cursor answers -32603).
  Per-session subprocesses are reaped on close_session / router Close.
  Beware: `--model` speaks a different id dialect
  (`claude-fable-5-thinking-high`) than ACP's decorated availableModels ids
  (`claude-fable-5[thinking=true,…]`); most decorated ids happen to parse
  but some make the process print "Cannot use this model" and exit mid-
  handshake ("acp client closed"). `buildAdapterForSession` retries with a
  translated spelling then the bare base (`modelSpawnCandidates`), and
  handshake errors now embed the subprocess's stderr tail so the cause is
  visible.
- **Modes**: cursor advertises `agent` / `plan` / `ask`; guest "ask" shares
  force `ask` the same way as Devin (`askModeID`), and guest permission
  requests are auto-denied for every agent.
- **Tool-call args are lost upstream**: cursor's ACP presenter (bundle
  `7414.index.js`) does extract per-tool args (`extractToolCallInput`: glob →
  pattern, read → path, shell → command, …) and its title templates would
  render ``Find `path` `pattern` `` — but it snapshots `args` at the internal
  `toolCallStarted` event, before the model has finished streaming them, and
  no `tool_call_update` ever re-sends `rawInput`/`title`. Net effect on the
  wire (verified by live probing): generic titles ("Read File", "Find"),
  `rawInput: {}`, no `locations`, only the completion update has `rawOutput`.
  Shell calls are the exception (command is complete when the card is
  created, so it lands in the title). The web UI treats an empty-object
  payload as absent (`hasMeaningfulPayload` in
  `apps/web/src/components/transcript.ts`) and shows a "该 agent 未上报调用参数"
  note instead of a confusing `{}`.
- e2e coverage: `TestChatCursorAgentModels` in
  `apps/relay/internal/server/chat_e2e_test.go`; adapter-level tests in
  `internal/agent/acpagent/adapter_test.go`.
- Live-fire smoke tests against the real binary (need `cursor-agent` on PATH
  and a completed `cursor-agent login`; excluded from normal runs via the
  `cursorlive` build tag; run them one at a time — they share the real
  cursor backend):
  `go test -tags cursorlive ./internal/chatrouter/ -run TestLiveCursor -v`
  (adapter only),
  `go test -tags cursorlive ./apps/relay/internal/server/ -run TestLiveCursorFullStack -v`
  (client → relay → daemon → real cursor-agent),
  `... -run TestLiveCursorGuestModelLock -v` (guest share with a locked
  model — generation verified via a "who created you" probe), and
  `... -run TestLiveCursorModelSwitch -v` (mid-session model switch:
  restart + session/load keeps the conversation, new model generates).

## Relay orientation

- CLI entry: `apps/relay/cmd/main.go` (flags: `-addr`, `-log-level`, `-version`)
- Config defaults/env parsing: `apps/relay/internal/server/config.go`
- HTTP middleware/CORS: `apps/relay/internal/server/middleware.go`
- Hub/routing: `apps/relay/internal/server/hub.go`. Clients and guests are
  keyed by **connection id**, so several tabs of one paired browser (same
  device_id) coexist instead of evicting each other into a reconnect storm.
  Daemons are keyed by device_id: a newer daemon connection replaces the old
  one (close reason `"replaced"` — if a daemon log shows this, a second
  daemon process is running with the same credentials file).
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
  - 'traefik.enable=true'
  - 'traefik.http.routers.ca-relay.rule=Host(`relay.example.com`)'
  - 'traefik.http.routers.ca-relay.entrypoints=websecure'
  - 'traefik.http.routers.ca-relay.tls.certresolver=le'
  - 'traefik.http.services.ca-relay.loadbalancer.server.port=8443'
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
