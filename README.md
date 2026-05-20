# coding-anywhere

Most AI coding agents run locally. **Coding Anywhere** lets you remotely
control a development terminal — and the AI coding agents running inside it —
from any phone or computer, so you can program and manage services from
anywhere.

Priority order for agent integrations:

1. **Devin for Terminal** (highest priority, ACP-native)
2. Claude Code
3. Codex
4. cursor-agent

## Architecture

```
┌──────────────────┐        ┌──────────────────┐        ┌──────────────────┐
│   Client         │        │   Relay (VPS)    │        │  Dev machine     │
│   - Web (React)  │ <─wss─>│   - Go           │<─wss─> │  - daemon (Go)   │
│   - Mobile (RN)  │        │   - pairing/JWT  │        │  - PTY mux       │
│     [v3]         │        │   - routing      │        │  - Devin (ACP)   │
└──────────────────┘        └──────────────────┘        │  - files / ports │
                                                        └──────────────────┘
```

Both the daemon and the client connect **outbound** to the relay over WSS.
No inbound port is opened on the dev machine. The relay only routes
messages — it does not store conversation content.

## Repository layout

```
coding-anywhere/
├── apps/
│   ├── daemon/      Go: runs on the dev machine
│   ├── relay/       Go: deploy to a small VPS via docker-compose
│   ├── web/         React + Vite + TypeScript + Mantine
│   └── mobile/      [v3] React Native (Expo) sharing packages/*
├── packages/
│   ├── protocol/    Wire envelope + zod schemas (TS source of truth)
│   ├── state/       zustand stores shared by web + mobile
│   ├── tokens/      Design tokens (used by Mantine + NativeWind)
│   ├── agents/      Per-agent metadata (Devin, Claude Code, ...)
│   └── utils/       Shared utilities (ulid, errors)
├── internal/        Go packages (wire, ptymux, agent/devin, auth, files, ...)
├── proto/           JSON Schemas — single source of truth for the wire
├── tools/codegen/   Generates TS + Go types from proto/
├── docker/relay/    Dockerfile + compose + Caddyfile for self-hosting
└── docs/
```

## Prerequisites

- Node.js ≥ 22 (`.nvmrc` pins 22)
- pnpm ≥ 10
- Go ≥ 1.24
- (Self-hosting the relay) Docker + docker-compose, plus a public DNS name

## Getting started

```bash
# Install JS deps and tidy Go modules
make install

# Start web + daemon + relay together
make dev

# Build everything
make build

# Run all tests (Go + JS)
make test

# Lint / format
make lint
make fmt
```

The web dev server runs on <http://localhost:5173> by default. The relay
listens on `:8443` and the daemon dials it; both can be reconfigured via
flags in P1.

## Self-hosting the relay

```bash
cd docker/relay
cp .env.example .env
# edit .env: set CA_DOMAIN to a DNS name pointing at this host
#            set CA_RELAY_JWT_SECRET to a random base64 string
docker compose up -d
```

Caddy fetches a Let's Encrypt cert automatically on first request.

## Roadmap (phased)

| Phase  | Scope                                                         |
| ------ | ------------------------------------------------------------- |
| **P0** | Repo scaffold, CI, codegen pipeline, Mantine UI shell         |
| **P1** | WS protocol + device pairing + JWT + echo loop                |
| **P2** | PTY multiplexer + xterm.js standalone terminal                |
| **P3** | Devin for Terminal adapter via `devin acp`                    |
| **P4** | Structured chat UI with session management & dual-mode toggle |
| **P5** | File browser + diff viewer (CodeMirror 6)                     |
| **P6** | Listening-port detection + service preview reverse proxy      |
| **P7** | Devin lifecycle hooks → Web Push notifications                |
| **P8** | CLI installer, system service, polished docs                  |
| v2     | Claude Code / Codex / cursor-agent adapters                   |
| v3     | React Native (iOS + Android) sharing `packages/*`             |

## License

MIT — see `LICENSE` (added with the first tagged release).
