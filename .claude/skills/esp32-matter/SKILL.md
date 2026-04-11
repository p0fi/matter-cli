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

This skill controls a physical ESP32-C6 Matter test device from any project.
The device project lives at `$ESP32_PROJECT_PATH` — all commands must cd there first.

> If `ESP32_PROJECT_PATH` is not set, check the controller project's `mise.toml`.

## Running Tasks

All tasks use `mise run` inside the device project directory:

```bash
cd "$ESP32_PROJECT_PATH" && mise run build
cd "$ESP32_PROJECT_PATH" && mise run flash-monitor
cd "$ESP32_PROJECT_PATH" && mise run logs
```

| Task | When to use |
|---|---|
| `mise run build` | After editing any firmware source file |
| `mise run flash-monitor` | Deploy + stream logs (preferred for iteration) |
| `mise run flash` | Deploy without opening monitor |
| `mise run monitor` | Watch logs without reflashing |
| `mise run erase` | Wipe flash before first flash or to reset state |
| `mise run logs` | Read last 100 lines of captured serial output |
| `mise run clean` | Delete build artefacts when build is broken |

## Iteration Loop

When the user asks you to change firmware behaviour, follow this loop:

1. Edit source files under `vendor/esp-matter/examples/light/main/`
2. Run `mise run build` — fix any compiler errors before proceeding
3. Run `mise run flash-monitor` — flash and capture output to `/tmp/esp32-logs.txt`
4. Run `mise run logs` — read the output and verify the change worked
5. Repeat until correct

Never skip the build step. Never flash if build has errors.

## Pairing Code
The manual pairing code for the device is 34970112332 or 20202021.

## Reading Logs

Serial output is captured to `/tmp/esp32-logs.txt` by `flash-monitor` and `monitor`.

```bash
mise run logs                    # last 100 lines
tail -f /tmp/esp32-logs.txt           # live stream
grep "ERROR\|WARN" /tmp/esp32-logs.txt
```

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
| `ESP_PORT` | Serial port of the connected board |
| `IDF_TARGET` | `esp32c6` |
| `ESP_LOG_FILE` | `/tmp/esp32-logs.txt` |

## Common Errors and Fixes

| Error | Cause | Fix |
|---|---|---|
| `CMakeLists.txt not found` | Running idf.py from wrong dir | Always use `mise run` tasks |
| `ninja: unknown target` | Port glob matched wrong device | Check `ESP_PORT` in `mise.toml` |
| `No such file /dev/tty...` | Board not connected | Replug USB, verify port |
| `Permission denied` on port | USB permission issue | Unplug and replug |
| Stale build errors after config change | Cached build state | Run `mise run clean` then `mise run build` |
| BLE advertising not seen in logs | First boot after commission | Run `mise run erase` to reset state |
