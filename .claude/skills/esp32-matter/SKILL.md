---
name: esp32-matter-device
description: >
  Use this skill when working with the ESP32-C6 Matter device in this project.
  Triggers on any request to build, flash, monitor, or iterate on firmware,
  read device logs, or debug Matter commissioning. Use when the user mentions
  the ESP32, the light example, BLE commissioning, or serial output.
license: MIT
compatibility:
  claude-code: "*"
allowed-tools:
  - Bash
---

# ESP32-C6 Matter Test Device Skill

The device project lives at `/Users/hta1we/Developer/Matter/esp`.
All `mise run` commands must cd there first.

**Shorthand** used below:

```bash
ESP=/Users/hta1we/Developer/Matter/esp
```

## Quick Reference

```bash
cd $ESP && mise run build              # compile firmware
cd $ESP && mise run flash-monitor-bg   # flash + background monitor (non-blocking)
cd $ESP && mise run logs               # last 100 log lines
.claude/skills/esp32-matter/scripts/read-logs.sh session   # latest boot session
.claude/skills/esp32-matter/scripts/read-logs.sh errors    # recent errors/warnings
.claude/skills/esp32-matter/scripts/kill-monitor.sh        # kill monitor before flash/erase
.claude/skills/esp32-matter/scripts/device-port.sh         # detect current serial port
```

## Tasks

| Task | When to use |
|---|---|
| `build` | After editing any firmware source file |
| `flash-monitor-bg` | **Preferred** — flash + monitor in background, returns immediately |
| `flash-monitor` | Flash + monitor blocking (only if you need interactive TTY) |
| `flash` | Deploy without monitoring |
| `monitor` | Watch logs without reflashing |
| `logs` | Quick last 100 lines of captured serial output |
| `erase` | Wipe flash before first flash or to reset commissioning state |
| `clean` | Delete build artefacts when build is broken |

## Port Detection

The ESP32-C6 serial port can change between USB reconnections (e.g.
`/dev/tty.usbmodem1401` becomes `/dev/tty.usbmodem5ABA0040481`).
The `ESP_PORT` in `mise.toml` may be stale.

**Always detect the port before flashing/erasing:**

```bash
PORT=$(.claude/skills/esp32-matter/scripts/device-port.sh)
```

If the detected port differs from `ESP_PORT` in `mise.toml`, use esptool
directly instead of `mise run flash/erase` — idf.py caches the port in
CMakeCache and ignores env overrides:

```bash
ESPTOOL="/Users/hta1we/.espressif/python_env/idf5.4_py3.11_env/bin/python /Users/hta1we/Developer/Matter/esp/vendor/esp-idf/components/esptool_py/esptool/esptool.py"
BUILD=/Users/hta1we/Developer/Matter/esp/vendor/esp-matter/examples/light/build

# Erase
$ESPTOOL -p $PORT --chip esp32c6 erase_flash

# Flash
cd $BUILD && $ESPTOOL -p $PORT -b 460800 --before default_reset --after hard_reset --chip esp32c6 write_flash @flash_args
```

## Kill Monitor Before Flash/Erase

**The serial port can only be used by one process at a time.** If a
background monitor is running, flash and erase will fail with
`Resource busy`. Always kill the monitor first:

```bash
.claude/skills/esp32-matter/scripts/kill-monitor.sh
```

This kills `idf_monitor.py`, its `script` wrapper, and any stray
`esptool` processes, then waits 1 second for the port to release.

## Iteration Loop

When changing firmware behaviour:

1. Edit source files under `vendor/esp-matter/examples/light/main/`
2. `cd $ESP && mise run build` — fix compiler errors before proceeding
3. `.claude/skills/esp32-matter/scripts/kill-monitor.sh` — free the port
4. `cd $ESP && mise run flash-monitor-bg` — flash and start background monitor
5. Wait 3-5 seconds for boot, then read logs (see below)
6. Repeat until correct

Never skip the build step. Never flash if build has errors.

## Reading Logs

Serial output is captured to `/tmp/esp32-logs.txt`.

### Smart log reader (preferred)

The script at `.claude/skills/esp32-matter/scripts/read-logs.sh` provides several modes:

```bash
# Latest boot session only (filters out old runs)
.claude/skills/esp32-matter/scripts/read-logs.sh session

# Recent errors and warnings only
.claude/skills/esp32-matter/scripts/read-logs.sh errors

# Search for a specific pattern
.claude/skills/esp32-matter/scripts/read-logs.sh search "commissioning"

# Last N lines (default 100)
.claude/skills/esp32-matter/scripts/read-logs.sh last 200

# Raw tail (no header, good for piping)
.claude/skills/esp32-matter/scripts/read-logs.sh tail 50
```

**After flashing**: always use `session` to get only the current boot's output.
**When debugging**: use `errors` first — if that's not enough, fall back to `session`.

### Key log prefixes

| Prefix | Meaning |
|---|---|
| `I (ms) chip[SVR]` | Matter server — commissioning, QR code |
| `I (ms) chip[DL]` | Device layer — BLE advertising, WiFi join |
| `I (ms) chip[ZCL]` | Cluster events — on/off attribute changes |
| `E (ms)` | Errors — always investigate these |
| `W (ms)` | Warnings |

### Successful boot looks like

```
I (...) chip[SVR]: SetupQRCode: [MT:...]
I (...) chip[SVR]: Manual pairing code: XXXX-XXX-XXXX
I (...) chip[DL]: BLE advertising started
```

If you see the QR code and BLE advertising, the device is ready to commission.

## Pairing Code

Manual pairing code: **34970112332** or discriminator passcode: **20202021**

## Source Layout

```
vendor/esp-matter/examples/light/
├── main/
│   ├── app_main.cpp        ← entry point, cluster setup, callbacks
│   └── CMakeLists.txt
├── CMakeLists.txt
└── sdkconfig.defaults      ← build-time config, do not edit manually
```

For config changes use `idf.py menuconfig` — never edit `sdkconfig` directly.

## Environment (from mise.toml)

| Variable | Value |
|---|---|
| `IDF_PATH` | `vendor/esp-idf` |
| `ESP_MATTER_PATH` | `vendor/esp-matter` |
| `ESP_PORT` | `/dev/tty.usbmodem1401` (may change — use `device-port.sh`) |
| `IDF_TARGET` | `esp32c6` |
| `ESP_LOG_FILE` | `/tmp/esp32-logs.txt` |

## Common Errors and Fixes

| Error | Cause | Fix |
|---|---|---|
| `Resource busy` on port | Background monitor holds the port | Run `kill-monitor.sh` before flash/erase |
| `Could not open /dev/tty...` | Port changed after USB reconnect | Run `device-port.sh` to detect current port, use esptool directly |
| `CMakeLists.txt not found` | Running idf.py from wrong dir | Always use `mise run` tasks |
| `ninja: unknown target` | Port glob matched wrong device | Check `ESP_PORT` in mise.toml |
| `No such file /dev/tty...` | Board not connected | Replug USB, verify port |
| `Permission denied` on port | USB permission issue | Unplug and replug |
| Stale build errors after config change | Cached build state | Run `clean` then `build` |
| BLE advertising not seen in logs | First boot after commission | Run `erase` to reset state |
