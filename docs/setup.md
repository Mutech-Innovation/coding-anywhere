# Setting up Coding Anywhere

Coding Anywhere has three moving parts: a **relay** running on a small
VPS (or your LAN), a **daemon** running on each dev machine you want to
control, and a **web client** (your browser, or self-hosted at any HTTPS
URL).

This guide walks through:

1. Standing up the relay via `docker compose`.
2. Installing the daemon on macOS or Linux and pairing it to the relay.
3. Pointing your browser at the relay and pairing it.

## 1. Relay

### 1.1 Prerequisites

- A host with a public DNS name (e.g. `relay.example.com`).
- Ports `80/tcp` and `443/tcp` open inbound (Caddy will negotiate
  Let's Encrypt for you).
- Docker + Docker Compose v2.

### 1.2 Configure & launch

```bash
git clone https://github.com/Mutech-Innovation/coding-anywhere.git
cd coding-anywhere/docker/relay
cp .env.example .env
# Edit .env:
#   CA_DOMAIN=relay.example.com
#   CA_RELAY_JWT_SECRET=$(openssl rand -base64 48)
docker compose up -d --build
```

The relay's data lives in a Docker volume (`relay-data`); back it up if
you care about pairings, refresh tokens, and Web Push subscriptions
surviving across redeploys.

### 1.3 Quick smoke

```bash
curl https://relay.example.com/healthz
curl https://relay.example.com/v1/version
```

Both should return JSON.

### 1.4 LAN-only setup

For development against a local relay (no DNS, no TLS), use the binary
directly:

```bash
go build -o bin/coding-anywhere-relay ./apps/relay/cmd
CA_RELAY_JWT_SECRET=$(openssl rand -base64 48) \
  CA_RELAY_DATA_DIR=$PWD/relay-data \
  ./bin/coding-anywhere-relay -addr=:8443
```

The web client and daemon will both treat `http://your-host:8443` as the
relay URL.

## 2. Daemon

### 2.1 Install

For now, build from source:

```bash
go build -o ~/.local/bin/coding-anywhere-daemon ./apps/daemon/cmd
```

Make sure `~/.local/bin` is on your `$PATH`. Pre-built binaries land in P8.

### 2.2 Pair

The daemon's pairing is interactive: it prints a 6-digit code that you
type into the web app on a logged-in browser.

```bash
coding-anywhere-daemon init \
  --relay-url https://relay.example.com \
  --device-label "$(hostname)"
```

Output:

```
┌────────────────┐
│   381 957      │
└────────────────┘
Enter this code on the Coding Anywhere web/mobile app to finish pairing.
```

After the code is claimed, credentials are written to
`$XDG_DATA_HOME/coding-anywhere/credentials.json` (0600).

### 2.3 Run as a service

#### macOS (launchd)

Save this as `~/Library/LaunchAgents/dev.coding-anywhere.daemon.plist`,
adjusting paths as needed:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>dev.coding-anywhere.daemon</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/CHANGE_ME/.local/bin/coding-anywhere-daemon</string>
        <string>-workspace</string>
        <string>/Users/CHANGE_ME/projects</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/coding-anywhere-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/coding-anywhere-daemon.log</string>
</dict>
</plist>
```

```bash
launchctl load ~/Library/LaunchAgents/dev.coding-anywhere.daemon.plist
launchctl start dev.coding-anywhere.daemon
```

#### Linux (systemd user service)

Save this as `~/.config/systemd/user/coding-anywhere-daemon.service`:

```ini
[Unit]
Description=Coding Anywhere daemon
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/coding-anywhere-daemon -workspace %h/projects
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now coding-anywhere-daemon
journalctl --user -u coding-anywhere-daemon -f
```

## 3. Web client

The web app is a static React site. You can:

- Use the public deployment we host (when available), or
- Self-host on any static hosting service (Vercel, Cloudflare Pages,
  S3+CloudFront, GitHub Pages), or
- Run it locally during development:

```bash
cd apps/web
pnpm install
pnpm dev
```

The first time you load the app you'll see the **Pair this browser**
screen. Enter the same relay URL you gave the daemon, the 6-digit code
the daemon printed, and a label. Click **Pair**. After a successful
pair you land on the main app with five tabs:

- **Chat** — Devin sessions, with streaming responses, tool-call cards,
  and inline permission prompts.
- **Terminals** — open as many shells as you like, full xterm.js with
  resize, scrollback, multi-session sidebar.
- **Files** — read-only file tree + CodeMirror viewer.
- **Diff** — git change list with side-by-side merge view.
- **Ports** — listening localhost ports + an "Open" button that
  navigates to `https://relay/v1/proxy/<port>/<path>` so you can preview
  your dev server straight from the browser.

The bell icon in the top-right opens **Notifications**: enable Web Push
to be told when Devin finishes a turn even if the tab is in the
background.

## 4. Day-to-day commands

```bash
# Daemon
coding-anywhere-daemon                          # connect with stored creds
coding-anywhere-daemon init --relay-url ...     # re-pair
coding-anywhere-daemon -log-level=debug         # verbose
coding-anywhere-daemon -workspace ~/code/proj   # change file root

# Relay (compose)
docker compose -f docker/relay/compose.yml logs -f
docker compose -f docker/relay/compose.yml restart
docker compose -f docker/relay/compose.yml down
```
