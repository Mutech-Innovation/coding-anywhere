# Architecture

Coding Anywhere is a remote-control plane for a developer's local machine.
It exposes the dev machine's terminals, AI coding agents (Devin for
Terminal first; Claude Code / Codex / cursor-agent in v2), files, git
diff, and listening localhost ports to a paired browser (and, in v3, a
React Native mobile app) — without opening any inbound port on the dev
machine.

## Three components

```
┌──────────────────┐        ┌──────────────────┐        ┌──────────────────┐
│   Client         │        │   Relay (VPS)    │        │  Dev machine     │
│   - Web (React)  │ <─wss─>│   - Go           │<─wss─> │  - daemon (Go)   │
│   - Mobile (RN)  │        │   - pairing/JWT  │        │  - PTY mux       │
│     [v3]         │        │   - routing      │        │  - Devin (ACP)   │
└──────────────────┘        │   - HTTP proxy   │        │  - files / diff  │
                            │   - Web Push     │        │  - ports + fwd   │
                            └──────────────────┘        └──────────────────┘
```

- **Daemon** runs on the dev machine, dials the relay outbound over WSS.
  It owns: PTY multiplexer, ACP subprocess (`devin acp`), files API,
  diff API, ports detection, push emission.
- **Relay** is a small Go service deployed via docker-compose to a VPS.
  It pairs devices, signs JWTs, persists per-account state in sqlite,
  routes envelopes between daemon ↔ clients, proxies HTTP traffic to
  the daemon's localhost ports, and dispatches Web Push.
- **Client** is the web app today; React Native shares the same
  `packages/protocol` types and `packages/state` stores in v3.

## Networking

Both the daemon and the client connect **outbound** to the relay over
WSS. The dev machine therefore needs no inbound port, no UPnP, no NAT
traversal, no SSH tunnel. Each WebSocket frame is a JSON envelope (see
`packages/protocol/src/envelope.ts` and `internal/wire/envelope.go`):

```ts
{ id: string, type: 'req'|'res'|'event', channel: 'chat'|'terminal'|..., method: string, payload: unknown }
```

The relay's hub is a kind-aware fan-out: a daemon's events broadcast to
every client on the same account, a client's requests broadcast to every
daemon (today: at most one). Server-injected requests (the HTTP proxy
endpoint) ride a per-`Conn` `serverPending` map so the relay can wait
for a specific response synchronously.

## Channels

All envelopes carry a `channel` string. Each channel has its own typed
payload definitions, mirrored on both sides of the wire.

| Channel         | Methods (req)                                                                                          | Events                                                                                                       | Sources                                      |
| --------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | -------------------------------------------- |
| `meta`          | `ping`                                                                                                 | `pong` (as `res`)                                                                                            | relay self-handles                           |
| `terminal`      | `open`, `list`, `attach`, `detach`, `input`, `resize`, `close`                                         | `opened`, `closed`, `data`, `exit`                                                                           | daemon `terminalrouter` ↔ `ptymux`           |
| `chat`          | `agents`, `list_sessions`, `create_session`, `prompt`, `cancel`, `close_session`, `resolve_permission` | `session_created`, `session_closed`, `session_status`, `update`, `permission_request`, `permission_resolved` | daemon `chatrouter` ↔ ACP (`devin acp`)      |
| `files`         | `list_workspace`, `list`, `read`, `stat`                                                               | —                                                                                                            | daemon `filesrouter` ↔ `fileapi`             |
| `git`           | `list_changes`, `versions`                                                                             | —                                                                                                            | daemon `diffrouter` ↔ `diffapi` (git CLI)    |
| `ports`         | `list`, `fetch`                                                                                        | —                                                                                                            | daemon `portsrouter` ↔ `portsapi` (gopsutil) |
| `notifications` | —                                                                                                      | `send`                                                                                                       | daemon emits → relay dispatches via Web Push |

## Authentication

Two phases:

1. **Pairing** (`POST /v1/pair/init` → 6-digit code → `POST /v1/pair/claim`).
   The relay creates an `account` row and one `device` row per pair-end on
   first claim, or attaches to an existing account when one of the parties
   already has a Bearer.
2. **Tokens**. Each device gets:
   - An access token: HS256 JWT, 1h for clients, 30 days for daemons.
   - A refresh token: 32 random bytes, 24h for clients, 90d for daemons.
     Stored as SHA-256 hash; exchanged via `POST /v1/auth/refresh`.

The WebSocket endpoint (`GET /v1/ws/connect`) authenticates via Bearer
header (preferred) or `?token=` query param. The HTTP proxy endpoint
(`/v1/proxy/...`) accepts only `?token=` (so a freshly opened tab works).

## Data persistence

The relay stores **only routing/auth state**, never conversation content:

- `accounts`, `devices`, `pairings`, `refresh_tokens`
- `vapid_keys`, `push_subscriptions`

The daemon stores credentials at
`$XDG_DATA_HOME/coding-anywhere/credentials.json` (0600). The web client
keeps the credential bundle in `localStorage` under
`coding-anywhere/credentials`.

## Devin / ACP

We integrate Devin via the [Agent Client Protocol]
(https://agentclientprotocol.com): JSON-RPC 2.0 over the stdio of a
`devin acp` subprocess. The daemon's adapter
(`internal/agent/devin.Adapter`) handles the initialize handshake,
`session/new`, `session/prompt`, `session/cancel`, plus the agent-driven
`session/update` notifications and `session/request_permission`
requests. We deliberately do **not** advertise `fs.readTextFile` /
`fs.writeTextFile` capabilities yet — Devin would call back via `fs/*`
and we'd need to forward those to the user. P5's file browser sets the
stage; flipping the capability flag is a small follow-up.

The `chatrouter` translates between channel envelopes and the adapter:
ACP `session/update` payloads are forwarded verbatim as `chat.update`
events; permission requests become `chat.permission_request` events
that the user resolves with `chat.resolve_permission`.

In addition to the streaming chat surface, the chatrouter persists every
session/update it forwards (plus the user prompts it accepts) into a
per-session in-memory snapshot tagged with a monotonically-increasing
sequence id. Two chat methods read this state:

- `chat.get_history` returns the snapshot — a list of raw ACP `update`
  bodies and the user-prompt entries — so a fresh client (browser
  refresh, second device, etc.) can replay the timeline. The web store
  interleaves prompts and updates by sequence id when rebuilding.
- `chat.set_mode` round-trips an ACP `session/set_mode`, letting the
  client switch between agent modes (e.g. `ask` ↔ `code` ↔ `plan`).
  The agent's `current_mode_update` notifications mutate the session
  back to the latest authoritative mode.

The chatrouter also captures the `availableModes` field that ACP agents
return on `session/new`, surfaces them in the `chat.create_session`
response, and folds `current_mode_update` / `available_commands_update`
/ `session_info_update` notifications into the persisted session state.

The web client decodes ACP `tool_call` / `tool_call_update` updates into
a strongly-typed view (`title`, `kind`, `status`, `content[]`,
`locations[]`, `rawInput`, `rawOutput`) — the older "(tool)" placeholder
is gone. Slash commands the agent advertises via
`available_commands_update` show up in a typeahead in the chat input.
`/clear` and `/new` additionally wipe the local UI session state so the
transcript matches what Devin reports after starting a fresh session.

## Web Push

The relay generates a VAPID keypair lazily (on first
`/v1/push/vapid_public_key` request) and persists it in sqlite. Browsers
register `service-worker.js`, fetch the public key, call
`PushManager.subscribe`, and POST the subscription to
`/v1/push/subscribe` (Bearer auth). When the daemon's `chatrouter` flips
a session's status from `thinking` → `idle`, it emits a
`notifications.send` event; the relay's WS read loop sees it and fans
out to every active subscription on the account.

## Relevant test entry points

- `internal/auth`, `internal/wire`: unit tests on JWT and envelopes.
- `internal/relayclient`: mock-WS round trip + reconnect.
- `internal/ptymux`: real shell tests (echo command, stty resize,
  scrollback replay).
- `internal/acp`, `internal/agent/devin`: ACP framing + adapter against
  an in-process mock ACP server.
- `apps/relay/internal/server`: every channel has at least one
  end-to-end test that drives a paired daemon + a raw client through
  the relay (`TestTerminalEndToEnd`, `TestChatEndToEnd`,
  `TestFilesEndToEnd`, `TestPortsProxyEndToEnd`,
  `TestPushSubscribeAndUnsubscribeRoundTrip`, `TestPushTest`, …).
