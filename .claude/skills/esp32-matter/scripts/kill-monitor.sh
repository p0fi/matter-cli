#!/usr/bin/env bash
# Kill any running ESP32 serial monitor processes.
# Must be run before flash or erase — the port will be busy otherwise.
set -euo pipefail

KILLED=0

# idf_monitor.py (started by mise run monitor / flash-monitor-bg)
for pid in $(pgrep -f 'idf_monitor\.py' 2>/dev/null || true); do
  kill "$pid" 2>/dev/null && KILLED=$((KILLED + 1))
done

# script wrapper (used by flash-monitor-bg for pseudo-TTY)
for pid in $(pgrep -f 'script.*idf_monitor' 2>/dev/null || true); do
  kill "$pid" 2>/dev/null && KILLED=$((KILLED + 1))
done

# esptool (shouldn't normally be running, but just in case)
for pid in $(pgrep -f 'esptool' 2>/dev/null || true); do
  kill "$pid" 2>/dev/null && KILLED=$((KILLED + 1))
done

if [ "$KILLED" -gt 0 ]; then
  sleep 1  # let the port release
  echo "Killed $KILLED monitor process(es)"
else
  echo "No monitor processes running"
fi
