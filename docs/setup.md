# Setting up Coding Anywhere

Coding Anywhere has three moving parts: a **relay** running on a small
VPS (or your LAN), a **daemon** running on each dev machine you want to
control, and a **web client** (your browser, or self-hosted at any HTTPS
URL).

This guide walks through:

1. Standing up the relay via `docker compose` (or a bare-metal binary + Caddy).
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

### 1.5 Bare-metal public deploy (no Docker)

If the host has no Docker, run the relay as a plain binary behind a native
Caddy. The relay is a **static, CGO-free Go binary**, so you can cross-compile
it on your laptop and copy it over — nothing (no Go, no Docker) needs to be
installed on the server.

**1. Cross-compile and upload.** Match the server's architecture
(`uname -m`: `x86_64` → `amd64`, `aarch64` → `arm64`):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o coding-anywhere-relay ./apps/relay/cmd
scp coding-anywhere-relay root@RELAY_HOST:/usr/local/bin/coding-anywhere-relay
```

> A mismatched arch fails at startup with `Exec format error` (systemd status
> `203/EXEC`). Confirm with `file coding-anywhere-relay` vs `uname -m`.

**2. Secret + systemd service.** On the server:

```bash
sudo mkdir -p /etc/coding-anywhere
echo "CA_RELAY_JWT_SECRET=$(openssl rand -base64 48)" | sudo tee /etc/coding-anywhere/relay.env >/dev/null
sudo chmod 600 /etc/coding-anywhere/relay.env
```

Copy [`systemd.coding-anywhere-relay.service`](./systemd.coding-anywhere-relay.service)
to `/etc/systemd/system/coding-anywhere-relay.service`, then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now coding-anywhere-relay
curl -s http://127.0.0.1:8443/healthz    # {"ok":true,...}
```

The relay binds loopback only; Caddy fronts it with TLS.

**3. Caddy (native) for TLS.** Install Caddy (see
<https://caddyserver.com/docs/install>), set `/etc/caddy/Caddyfile`:

```caddy
relay.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8443
}
```

```bash
sudo systemctl restart caddy
curl -s https://relay.example.com/healthz
```

Open ports **80** (Let's Encrypt HTTP-01 challenge) and **443** in the host
firewall / cloud security group.

**Sharing the box with something already on 443** (e.g. an Xray proxy). A port
has one listener; keep the other service on 443 and serve the relay on an
alternate HTTPS port. Caddy still gets a real cert via the HTTP-01 challenge on
port 80:

```caddy
{
	https_port 2096
}

relay.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8443
}
```

Open **80** and **2096** (leave 443 to the other service). Clients then address
the relay with the port: `wss://relay.example.com:2096`.

**Updating.** Stop first so the running binary isn't `Text file busy`:

```bash
sudo systemctl stop coding-anywhere-relay
scp coding-anywhere-relay root@RELAY_HOST:/usr/local/bin/   # from your laptop
sudo systemctl start coding-anywhere-relay
```

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

### 2.4 Headless / remote server (cross-compile + transfer)

For a dev box you only reach over SSH (e.g. a cloud VM in another region),
build the daemon on your laptop and copy the binary over — it's a static,
CGO-free binary, so the server needs no Go toolchain.

**1. Cross-compile and upload** (match the server's `uname -m`: `x86_64` →
`amd64`, `aarch64` → `arm64`):

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o coding-anywhere-daemon ./apps/daemon/cmd
ssh user@DEV_HOST 'mkdir -p ~/.local/bin'
scp coding-anywhere-daemon user@DEV_HOST:~/.local/bin/coding-anywhere-daemon
```

**2. Pair it** over SSH. `init` prints a 6-digit code and waits; type that
code into any logged-in web client (run locally per §3, or your hosted
deployment) to claim it. Include the relay's port if it isn't on 443:

```bash
~/.local/bin/coding-anywhere-daemon init \
  --relay-url wss://relay.example.com:2096 \
  --device-label "silicon-valley"
```

**3. Attach to your username/password login** so this machine appears when you
sign in from any browser. Run once per machine with the _same_ username +
password; the first machine creates the login, the rest attach to it:

```bash
~/.local/bin/coding-anywhere-daemon user register --username me
```

**4. Keep it running** as a systemd _system_ service — robust on a headless
box (starts at boot, no `loginctl enable-linger` dance). Run the block below
**as the user that paired in step 2** (the inner `sudo` only elevates the file
write); it bakes that user and its paths into the unit:

```bash
U=$(whoami); H="$HOME"
sudo tee /etc/systemd/system/coding-anywhere-daemon.service >/dev/null <<EOF
[Unit]
Description=Coding Anywhere daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$U
Environment=CA_CREDENTIALS_FILE=$H/.local/share/coding-anywhere/credentials.json
Environment=PATH=$H/.local/bin:/usr/local/bin:/usr/bin:/bin
ExecStart=$H/.local/bin/coding-anywhere-daemon -workspace $H
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now coding-anywhere-daemon
sudo systemctl status coding-anywhere-daemon --no-pager -l
```

`CA_CREDENTIALS_FILE` and `PATH` are pinned so the service finds the
credentials from step 2 and the agent CLIs even under systemd's minimal
environment. Point `-workspace` at the directory you want the Files/Diff tabs
rooted at.

> Terminals, files, diff, and ports work immediately. **Chat** additionally
> needs the agent CLI installed and authenticated on this machine (`devin`, or
> `cursor-agent login`).
>
> Prefer a _user_ service? Use the §2.3 unit plus `loginctl enable-linger
"$USER"` so it survives after you log out of SSH.

## 3. Web client

The web app is a static React site. You can:

- Use the public deployment we host (when available), or
- Self-host on any static hosting service (Vercel, Cloudflare Pages,
  S3+CloudFront, GitHub Pages), or
- Self-host on your own server behind Traefik (see §3.1), or
- Run it locally during development:

```bash
cd apps/web
pnpm install
pnpm dev
```

Whatever you choose, three things must hold:

- **Serve it over HTTPS.** The service worker (Web Push) and a real wss relay
  both require a secure context. An HTTPS page also *cannot* dial a plaintext
  `ws://` relay, so pair it with a `wss://` relay.
- **Serve it from the domain root**, not a sub-path — `index.html` references
  assets, the manifest, and `/service-worker.js` with absolute paths.
- **Fall back to `index.html`** for unknown paths. The guest share page lives at
  `/guest` and is the same `index.html` branching on the URL, so SPA-style
  fallback is required.

The relay URL is *not* baked into the build — you type it on the pair / login
screen at runtime — so the same static bundle works against any relay.

### 3.1 Self-host behind an existing Traefik (Docker)

If your server already runs Traefik (Docker provider + an ACME cert resolver),
`docker/web/` ships a tiny nginx image (SPA fallback + correct cache headers
baked in) wired to Traefik by labels.

```bash
cd coding-anywhere/docker/web
cp .env.example .env
# Edit .env:
#   CA_WEB_DOMAIN=app.example.com
#   CA_TRAEFIK_NETWORK / CA_TRAEFIK_ENTRYPOINT / CA_TRAEFIK_CERTRESOLVER
#     -> must match the names in your existing Traefik install
docker compose up -d --build
```

Point an A/AAAA record for `CA_WEB_DOMAIN` at the host; Traefik issues the cert
on first request. Then open `https://app.example.com`, and on the pair / login
screen enter your relay (e.g. `wss://relay.example.com`).

> The web app and the relay can live on different hosts and different
> sub-domains — the relay reflects the request `Origin` in its CORS headers and
> auth rides in a Bearer token (no cookies), so cross-origin works out of the
> box.

Updating to a new build: `docker compose up -d --build` again (re-runs the
bundle and replaces the container). Logs: `docker compose logs -f`.

The first time you load the app you'll see the **Pair this browser**
screen. Enter the same relay URL you gave the daemon, the 6-digit code
the daemon printed, and a label. Click **Pair**. After a successful
pair you land on the main app with five tabs:

- **Chat** — agent sessions (Devin for Terminal or cursor-agent), with
  streaming responses, tool-call cards, a model picker, and inline
  permission prompts. The agent CLI must be installed + authenticated on
  the daemon machine: `devin` for Devin, `cursor-agent` (run
  `cursor-agent login` once) for Cursor.
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
coding-anywhere-daemon user register --username me   # create/attach the login
coding-anywhere-daemon -log-level=debug         # verbose
coding-anywhere-daemon -workspace ~/code/proj   # change file root

# Relay (compose)
docker compose -f docker/relay/compose.yml logs -f
docker compose -f docker/relay/compose.yml restart
docker compose -f docker/relay/compose.yml down

# Relay (bare-metal / systemd)
sudo systemctl restart coding-anywhere-relay
journalctl -u coding-anywhere-relay -f
journalctl -u caddy -f
```
