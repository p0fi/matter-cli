#!/usr/bin/env bash
# Detect the ESP32-C6 serial port.
# The device uses a CP2102N or built-in USB-JTAG, which appears as
# /dev/tty.usbmodem* on macOS. If multiple ports match, prefer the
# one in ESP_PORT if it exists, otherwise return the first match.
set -euo pipefail

CONFIGURED="${ESP_PORT:-}"

# Find all candidate ports
PORTS=( $(ls /dev/tty.usbmodem* 2>/dev/null || true) )

if [ ${#PORTS[@]} -eq 0 ]; then
  echo "ERROR: No /dev/tty.usbmodem* ports found. Is the ESP32 connected?" >&2
  exit 1
fi

# If configured port exists, use it
if [ -n "$CONFIGURED" ]; then
  for p in "${PORTS[@]}"; do
    if [ "$p" = "$CONFIGURED" ]; then
      echo "$p"
      exit 0
    fi
  done
  echo "WARNING: Configured ESP_PORT=$CONFIGURED not found. Available: ${PORTS[*]}" >&2
fi

# Return the first available port
echo "${PORTS[0]}"
