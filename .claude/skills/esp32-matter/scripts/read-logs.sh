#!/usr/bin/env bash
# Read last N lines of ESP32 serial log
# Usage: read-logs.sh [lines]
LINES=${1:-100}
LOG_FILE="${ESP_LOG_FILE}"

if [ ! -f "$LOG_FILE" ]; then
  echo "No log file found at $LOG_FILE. Run: mise run flash-monitor"
  exit 1
fi

echo "=== Last $LINES lines from $LOG_FILE ==="
tail -n "$LINES" "$LOG_FILE"
