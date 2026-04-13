# BLE Commissioning Bugs

Bugs found and fixed during end-to-end BLE commissioning with an ESP32-C6 device.

## Bug #1: Missing BTP Continue flag on non-first segments

**Symptom:** ESP32 logged `BLE_ERROR_REASSEMBLER_MISSING_DATA` when receiving multi-segment BTP messages (e.g. AddNOC, which exceeds a single BLE MTU).

**Root cause:** `buildSegment()` in `internal/transport/btp.go` did not set the `Continue` flag (`0x02`) on all non-first segments. In particular, the last segment of a multi-segment message was sent with only `End`, but the Matter BTP behavior used by the implementation requires `Continue` on every segment after the first, including the last.

**Fix:** Added `btpFlagContinue = 0x02` constant and set it in `buildSegment()` on every non-first segment, including the final segment.

## Bug #2: CCCD unsubscribe in WaitForNotifying destroys BTP session

**Symptom:** Device logged `selected BTP version 4` then `Releasing end point's BLE connection back to application` 610ms later. BTP handshake timed out on the CLI side.

**Root cause:** The WaitForNotifying repair path in `internal/transport/ble_adapter_tinygo.go` sent `setNotifyValue:NO` (CCCD unsubscribe) before retrying `setNotifyValue:YES`. The CHIP BLE stack on the device treats any CCCD unsubscribe as a terminal event -- it calls `HandleSubscribeOpReceived` with `Unsubscribe`, which tears down the BTP endpoint and closes the connection.

In practice, tinygo's initial subscribe write usually *does* reach the device (the device logs "BLE connection established"), but CoreBluetooth's local `isNotifying` flag hasn't updated because tinygo calls `setNotifyValue` from a Go goroutine thread instead of on cbgo's `bt_queue`.

**Fix:** Removed the `corebtUnsubscribe` call and the 150ms sleep from the repair path. The retry now sends only `setNotifyValue:YES` on `bt_queue` -- if the first write already reached the device, this is a harmless duplicate; if not, it's the actual recovery.

## Bug #3: grandcat/zeroconf mDNS broken on macOS

**Symptom:** `WatchOperational` found zero devices after 3 minutes. Manual testing with `dns-sd -B _matter._tcp local.` found 12 devices instantly. Even a raw `net.ListenMulticastUDP` on port 5353 received zero packets.

**Root cause:** macOS's `mDNSResponder` daemon monopolizes UDP port 5353. While Go can open a multicast socket with `SO_REUSEPORT`, it receives no multicast traffic -- mDNSResponder consumes it all. The `grandcat/zeroconf` library relies on Go's multicast socket, so it is fundamentally broken for receiving unsolicited mDNS announcements on macOS.

Note: zeroconf *can* discover devices that were already present when the browse started, because those devices respond to the query via unicast directly to zeroconf's source port. But it cannot discover devices that appear *after* the browse starts (the exact scenario during BLE commissioning, when the device just joined WiFi).

**Fix:** Created `internal/discovery/mdns_dnssd_darwin.go` -- a macOS-native resolver that shells out to the `dns-sd` command, which communicates with mDNSResponder via IPC. Split `NewMDNSBrowser()` into platform-specific files (`mdns_browser_darwin.go` and `mdns_browser_other.go`) using the `_darwin` / `!darwin` filename convention.

## Bug #4: Sequential dns-sd instance resolution blocks browse

**Symptom:** mDNS discovery took 90+ seconds to find the target device even though it appeared in the browse output within 15 seconds. Each unrelated instance took 5 seconds to resolve via `dns-sd -L` and `dns-sd -G`, and all were resolved sequentially before the browse scanner could process the next line.

**Root cause:** The `dnssdResolver.Browse()` method called `resolveInstance()` synchronously for every instance found. With 9 unrelated Matter devices on the network, that's 45 seconds of blocking resolution before the target device could even be processed.

**Fix:** Resolve instances concurrently using goroutines. Each new instance spawns a goroutine for `resolveInstance()`; results are sent to the entries channel as they complete. A `sync.WaitGroup` ensures all in-flight resolutions finish before the channel is closed.

## Bug #5: Commissioned node saved with BLE address instead of IP

**Symptom:** After successful BLE commissioning, `matter OnOff Toggle @N/1` failed with `lookup udp///662eef0c-...: unknown port` because the node's `last_address` was a `ble://` URL instead of the operational IP.

**Root cause:** The commissioning flow at `internal/commissioning/flow.go:599` stored the original `addr` in `CommissioningResult.Address`. For BLE commissioning, this was the BLE discovery address (`ble://...`), not the IP address used for the CASE session.

**Fix:** Added `RemoteAddress() string` to the `Session` interface. The `controllerSession` wrapper now stores the address passed to `EstablishPASE`/`EstablishCASE`. After CASE completes, the commissioning flow uses `caseSession.RemoteAddress()` for the result address, which is the operational IP:port.
