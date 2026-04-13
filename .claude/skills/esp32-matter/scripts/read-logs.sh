#!/usr/bin/env bash
# Smart ESP32 log reader for LLM agents
# Usage: read-logs.sh [command] [args...]
#   read-logs.sh last [N]         — last N lines (default 100)
#   read-logs.sh session          — extract the most recent boot session
#   read-logs.sh errors [N]       — last N errors/warnings (default 30)
#   read-logs.sh search <pattern> — grep for a pattern in recent logs
#   read-logs.sh tail [N]         — last N lines, no header (raw)
set -euo pipefail

LOG_FILE="${ESP_LOG_FILE:-/tmp/esp32-logs.txt}"

if [ ! -f "$LOG_FILE" ]; then
  echo "No log file found at $LOG_FILE. Run: mise run flash-monitor-bg"
  exit 1
fi

CMD="${1:-last}"
shift 2>/dev/null || true

case "$CMD" in
  last)
    N="${1:-100}"
    echo "=== Last $N lines ==="
    tail -n "$N" "$LOG_FILE"
    ;;
  session)
    # Extract from the most recent reboot marker to end of file
    # ESP32 prints "rst:0x1" or "boot:" near the start of each boot
    BOOT_LINE=$(grep -n 'rst:0x\|entry 0x\|Boot image\|cpu_start: Multicore' "$LOG_FILE" | tail -1 | cut -d: -f1)
    if [ -z "$BOOT_LINE" ]; then
      echo "No boot marker found — showing last 150 lines"
      tail -n 150 "$LOG_FILE"
    else
      TOTAL=$(wc -l < "$LOG_FILE")
      REMAINING=$((TOTAL - BOOT_LINE + 1))
      # Cap at 300 lines to avoid flooding context
      if [ "$REMAINING" -gt 300 ]; then
        echo "=== Latest boot session (last 300 of $REMAINING lines) ==="
        tail -n 300 "$LOG_FILE"
      else
        echo "=== Latest boot session ($REMAINING lines from line $BOOT_LINE) ==="
        tail -n +"$BOOT_LINE" "$LOG_FILE"
      fi
    fi
    ;;
  errors)
    N="${1:-30}"
    echo "=== Last $N errors/warnings ==="
    grep -i 'E (\|W (\|ERROR\|WARN\|FAIL\|panic\|abort\|assert' "$LOG_FILE" | tail -n "$N"
    ;;
  search)
    PATTERN="${1:?Usage: read-logs.sh search <pattern>}"
    echo "=== Matches for '$PATTERN' ==="
    grep -i "$PATTERN" "$LOG_FILE" | tail -n 50
    ;;
  tail)
    N="${1:-50}"
    tail -n "$N" "$LOG_FILE"
    ;;
  *)
    echo "Unknown command: $CMD"
    echo "Usage: read-logs.sh {last|session|errors|search|tail} [args]"
    exit 1
    ;;
esac
