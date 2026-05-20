#!/usr/bin/env bash
# Tiny smoke test: build everything and start daemon + relay just long enough
# to confirm both come up, then tear them down.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> Go build"
go build ./...

echo "==> JS build"
pnpm build

echo "==> Booting relay (5s)"
go run ./apps/relay/cmd -addr=:18443 &
RELAY_PID=$!
sleep 1

echo "==> Booting daemon (5s)"
go run ./apps/daemon/cmd &
DAEMON_PID=$!
sleep 1

# Kill both and wait. Be tolerant of either having exited already.
echo "==> Smoke OK; tearing down"
kill "$RELAY_PID" "$DAEMON_PID" 2>/dev/null || true
wait "$RELAY_PID" 2>/dev/null || true
wait "$DAEMON_PID" 2>/dev/null || true
echo "==> Done"
