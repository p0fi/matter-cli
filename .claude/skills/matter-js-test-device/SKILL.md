---
name: matter-js-test-device
description: >
  Spin up a matter.js virtual On/Off light device for testing matter-cli
  commissioning and cluster commands. Triggers when the user asks to start
  a test device, spin up a virtual device, or needs something to commission against.
license: MIT
compatibility:
  claude-code: "*"
allowed-tools:
  - Bash
---

# matter.js Test Device Skill

This skill spins up a virtual Matter On/Off light using matter.js for testing `matter-cli`.

## Quick Start

```bash
cd /tmp && mkdir -p matter-js-test && cd matter-js-test
npm init @matter examples-device-onoff
npm run app
```

The device will print a QR code and manual pairing code on startup. Use these to commission with `matter-cli`.

## Expected Startup Output

```
matter.js Device Shell
...
QR Code URL: https://project-chip.github.io/connectedhomeip/qrcode.html?data=MT:...
Manual pairing code: XXXX-XXX-XXXX
```

Once you see the pairing code, the device is ready to commission.

## Commissioning the Test Device

From the matter-cli project:

```bash
matter commission code "MT:..."
# or
matter commission code XXXX-XXX-XXXX
```

## Resetting the Device

Delete the stored state and restart to make it commissionable again:

```bash
npm run app -- --storage-clear
```

## Notes

- The device runs on the local network — no hardware required.
- It advertises via mDNS as a commissionable device.
- Supports On/Off cluster on endpoint 1.
- Stop it with Ctrl+C.
