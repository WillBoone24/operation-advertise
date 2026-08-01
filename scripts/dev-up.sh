#!/usr/bin/env bash
# dev-up.sh
# -----------------------------------------------------------------------
# One command to start a full dev session:
#   1. Detects the current WSL IP and regenerates both frontends'
#      config.js files to point at it (see write-dev-config.sh).
#   2. Kills any stale backend process still holding port 8080 (the
#      "bind: address already in use" problem from a leftover `go run`
#      child process surviving a Ctrl+C).
#   3. Starts the backend, resume-frontend, and rpg-frontend, all
#      detached via nohup so closing this terminal doesn't kill them.
#
# Run this from anywhere; paths are resolved relative to the script's
# own location, not your current directory.
#
# Usage:
#   ./scripts/dev-up.sh
#
# To stop everything later: ./scripts/dev-down.sh
# -----------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Step 1: regenerate frontend config with the current WSL IP ---
source "$SCRIPT_DIR/write-dev-config.sh"
# $DEV_IP is now set, exported by write-dev-config.sh above.

# --- Step 2: clear anything stale off port 8080 ---
# `go run` spawns a separate compiled binary as a child process;
# killing `go run` itself doesn't always kill that child, which is
# exactly what caused "bind: address already in use" earlier. Finding
# and killing whatever actually holds the port, rather than just the
# `go run` wrapper, avoids that.
EXISTING_PID="$(ss -tlnp 2>/dev/null | grep ':8080' | grep -oP 'pid=\K[0-9]+' || true)"
if [ -n "$EXISTING_PID" ]; then
  echo "dev-up: killing stale process on port 8080 (pid $EXISTING_PID)"
  kill -9 "$EXISTING_PID" 2>/dev/null || true
  sleep 1
fi

# --- Step 3: start the backend ---
cd "$REPO_ROOT/backend"

# NOTE: JWT_SECRET is a real secret in production. This fixed dev value
# is fine for local development only — never reuse it anywhere real.
export JWT_SECRET="${JWT_SECRET:-dev-secret-change-me-please-make-it-longer-1234}"
export ALLOWED_ORIGINS="http://${DEV_IP}:5173,http://${DEV_IP}:3000,http://localhost:5173,http://localhost:3000,http://127.0.0.1:5173,http://127.0.0.1:3000"

nohup go run ./cmd/server > "$REPO_ROOT/backend/server.log" 2>&1 &
disown
echo "dev-up: backend starting (log: $REPO_ROOT/backend/server.log)"

# --- Step 4: start both frontends ---
# Using nocache_server.py instead of plain `http.server` — it sends
# Cache-Control: no-store on every response, so the browser can never
# serve a stale cached copy of main.js/state.js on a normal reload.
# (Process name still contains "http.server 5173"/"http.server 3000",
# so dev-down.sh's pgrep pattern still matches — no change needed there.)
cd "$REPO_ROOT/resume-frontend"
nohup python3 "$SCRIPT_DIR/nocache_server.py" 5173 > /tmp/resume-frontend.log 2>&1 &
disown
echo "dev-up: resume-frontend starting on :5173 (http.server, no-cache)"

cd "$REPO_ROOT/rpg-frontend"
nohup python3 "$SCRIPT_DIR/nocache_server.py" 3000 > /tmp/rpg-frontend.log 2>&1 &
disown
echo "dev-up: rpg-frontend starting on :3000 (http.server, no-cache)"

# --- Step 5: give the backend a moment, then confirm it's actually up ---
sleep 2
if curl -sf "http://localhost:8080/health" > /dev/null; then
  echo ""
  echo "dev-up: all three servers are up."
  echo "dev-up: open these in your Windows browser (not the localhost equivalents):"
  echo "  http://${DEV_IP}:5173   (resume-frontend)"
  echo "  http://${DEV_IP}:3000   (rpg-frontend)"
else
  echo ""
  echo "dev-up: WARNING — backend did not respond to a health check." >&2
  echo "dev-up: check $REPO_ROOT/backend/server.log for what went wrong." >&2
fi
