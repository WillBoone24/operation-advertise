#!/usr/bin/env bash
# dev-down.sh — stops the backend and both frontend static servers
# started by dev-up.sh. Safe to run even if some/none are running.
set -uo pipefail

kill_matching() {
  local pattern="$1"
  local label="$2"
  local pids
  pids="$(pgrep -f "$pattern" || true)"
  if [ -n "$pids" ]; then
    echo "dev-down: stopping $label (pid(s): $pids)"
    kill -9 $pids 2>/dev/null || true
  else
    echo "dev-down: $label not running"
  fi
}

# Kill whatever is actually bound to 8080 (catches the go run child
# binary directly, same reasoning as dev-up.sh's cleanup step).
PORT_PID="$(ss -tlnp 2>/dev/null | grep ':8080' | grep -oP 'pid=\K[0-9]+' || true)"
if [ -n "$PORT_PID" ]; then
  echo "dev-down: stopping backend (pid $PORT_PID)"
  kill -9 "$PORT_PID" 2>/dev/null || true
else
  echo "dev-down: backend not running"
fi

kill_matching "nocache_server.py 5173" "resume-frontend"
kill_matching "nocache_server.py 3000" "rpg-frontend"

echo "dev-down: done"
