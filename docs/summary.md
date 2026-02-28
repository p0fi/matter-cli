# BLE Commissioning — Debugging Summary

This document records the bugs found and fixed during BLE commissioning development, in chronological order.

---

## 1. BTP Handshake Bug Fix

### Problem

BLE commissioning failed during the BTP (Bluetooth Transport Protocol) handshake. The device disconnected ~3 seconds after the handshake request was sent, and all retry attempts failed:

```
✗ Commissioning failed: commissioning: establishing PASE session: controller: ble: BTP handshake timed out (tried 6 attempts)
```

### Root Cause

Two bugs in `internal/transport/ble.go`, both deviating from the CHIP SDK's BTP handshake implementation:

**Bug 1: Wrong GATT write type.** The BTP Capabilities Request was sent via GATT Write Without Response (`ATT_WRITE_CMD`, opcode `0x52`), but the Matter specification (§4.18) and the CHIP SDK require a GATT Write Request (`ATT_WRITE_REQ`, opcode `0x12`). The C1 characteristic's spec-defined property is "Write" (not "Write Without Response"), so devices reject the wrong write type.

**Bug 2: Wrong operation ordering.** The code subscribed to C2 (CCCD write) **before** sending the Capabilities Request write to C1. The CHIP SDK does the opposite. This matters because the peripheral's state machine creates its BTP endpoint only when the C1 write arrives. If subscribe arrives first, no endpoint exists yet, the subscribe is silently dropped, and the device eventually times out waiting for a subscribe that already came and went.

### Fix

Changed `DialBLE()` to match chip-tool's exact handshake sequence:

| Step | Before (broken) | After (correct, matches chip-tool) |
|------|------------------|------------------------------------|
| 1 | Subscribe to C2 (CCCD write) | Write BTP Capabilities Request to C1 (`WriteWithResponse`) |
| 2 | Wait for subscribe confirmation | Wait 50 ms for peripheral to process |
| 3 | Write Capabilities Request to C1 (`WriteWithoutResponse`) | Subscribe to C2 (CCCD write) |
| 4 | Wait for response | Wait for subscribe confirmation |
| 5 | — | Peripheral sends stashed response via C2 indication |

### Files changed

- **`internal/transport/ble.go`** — Reordered handshake (write C1 first, then subscribe C2), always use `WriteWithResponse`, removed `canSendWriteWithoutResponse` polling logic and unused `unsafe` import.
- **`internal/transport/ble_test.go`** — Updated timing to account for the new ordering.

### Reference

- **Central flow**: `BLEEndPoint::StartConnect()` sends the write, `HandleHandshakeConfirmationReceived()` subscribes to C2 on write confirmation.
- **Peripheral flow**: `HandleWriteReceived()` creates endpoint, `HandleCapabilitiesRequestReceived()` stashes response, `HandleSubscribeReceived()` sends the stashed response.
- **Key guard**: `HandleSubscribeReceived()` requires an existing endpoint (`sBLEEndPointPool.Find(connObj)`), which only exists after the write is processed.
- Source: [`BLEEndPoint.cpp`](https://github.com/project-chip/connectedhomeip/blob/master/src/ble/BLEEndPoint.cpp) and [`BleLayer.cpp`](https://github.com/project-chip/connectedhomeip/blob/master/src/ble/BleLayer.cpp)

---

## 2. BLE→IP Transition Bug Fix — Message Pump Tight Loop

### Problem

After BLE commissioning completes (PASE + ArmFailsafe + Attestation + CSR + AddNOC all succeed over BLE), the commissioning flow transitions to CASE over IP. At this point, the terminal floods with thousands of lines per second:

```
00:16:38 WARN   controller: receive error  err=ble: connection closed
00:16:38 WARN   controller: receive error  err=ble: connection closed
00:16:38 WARN   controller: receive error  err=ble: connection closed
...
```

### Root Cause

Two bugs in the BLE-to-IP transition during `bleSessionEstablisher.EstablishCASE`:

**Bug 1: Message pump tight loop on closed BLE connection.** `ConnectPASEoverBLE` replaces the controller's `conn` with the BLE connection and starts a message pump goroutine reading from it. When `EstablishCASE` calls `bleConn.Close()`, the pump's `Receive()` call returns immediately with `"ble: connection closed"`. The pump checks `ctx.Err()` (which is nil — the context is still alive), logs a warning, and calls `Receive()` again — creating an infinite tight loop.

**Bug 2: Controller not transitioned back to UDP.** `EstablishCASE` closed the BLE connection but did **not** stop the controller's message pump, did **not** create a new UDP connection for IP-based CASE, and did **not** reset `c.cancel` to nil — so `ConnectCASE` saw `c.cancel != nil` and skipped starting a new pump. The controller tried to use the dead BLE connection for CASE messages too.

### Fix

**1. Sentinel error for permanent connection closure.** Added `transport.ErrConnClosed` as a sentinel error. Both `BLEConn` and `UDPConn` now wrap it in their `Receive()` and `Send()` error returns:

```go
// transport/conn.go
var ErrConnClosed = errors.New("connection closed")

// transport/ble.go — Receive
case <-c.closed:
    return nil, nil, fmt.Errorf("ble: %w", ErrConnClosed)
```

**2. Message pump exits on permanent closure.** `runMessagePump` now checks for `ErrConnClosed` and exits gracefully:

```go
if errors.Is(err, transport.ErrConnClosed) {
    slog.Debug("controller: connection closed, stopping message pump")
    return
}
```

**3. Clean BLE→IP transition.** `bleSessionEstablisher.EstablishCASE` now:

| Step | Before (broken) | After (correct) |
|------|------------------|-----------------|
| 1 | Close BLE connection | Stop controller's message pump (`cancel()` + wait) |
| 2 | Call `ConnectCASE` (uses dead BLE conn) | Close BLE connection (nothing reading from it) |
| 3 | — | Create fresh UDP connection, assign to `c.conn` |
| 4 | — | Reset `c.cancel = nil` so `ConnectCASE` starts new pump |
| 5 | — | Discover device on IP via mDNS `_matter._tcp` |
| 6 | — | Call `ConnectCASE` with discovered IP address |

### Files changed

- **`internal/transport/conn.go`** — Added `ErrConnClosed` sentinel error.
- **`internal/transport/ble.go`** — `Send`, `writeSegmentC1`, `Receive` use `fmt.Errorf("ble: %w", ErrConnClosed)`.
- **`internal/transport/udp.go`** — `Receive` uses `fmt.Errorf("transport: %w", ErrConnClosed)`.
- **`internal/controller/controller.go`** — `runMessagePump` checks `errors.Is(err, transport.ErrConnClosed)` and returns.
- **`internal/controller/controller_ble.go`** — `bleSessionEstablisher.EstablishCASE` stops pump, closes BLE, creates new UDP conn, resets cancel, discovers device via mDNS, then calls `ConnectCASE` with the IP address.

---

## 3. BLE Data Delivery Simplification — Remove C2 Data Poller

### Problem

Every incoming C2 indication appeared twice in the logs — once from the tinygo notification callback and once from the 10 ms cached-value data poller:

```
23:49:27 DEBUG  ble: C2 data received (notification callback)  len=31  hex=0d00011a...
23:49:27 DEBUG  ble: C2 data received (data poller)             len=31  hex=0d00011a...
```

The data poller (`startC2DataPoller`) was originally introduced as a reliability workaround because tinygo's `DidUpdateValueForCharacteristic` delegate uses pointer-based characteristic matching on macOS. If the `CBCharacteristic` pointer captured at discovery time became stale, the callback would never fire. The poller bypassed this by reading `CBCharacteristic.value` directly via CoreBluetooth's serial queue at 10 ms intervals.

However, testing proved the notification callback fires reliably for **every** indication throughout the entire commissioning flow. The poller was redundant — adding CPU overhead, log noise, and unnecessary complexity.

### Root Cause

No actual bug — the data poller was a defensive workaround that turned out to be unnecessary. The notification callback never missed a single indication in testing, and data was only processed once (the callback only logged; the poller called `btp.handleSegment`). But the dual-path architecture was confusing and wasteful.

### Fix

**1. Notification callback is now the sole data delivery path.** An `atomic.Bool` (`dataMode`) gates the callback:

- **During handshake** (`dataMode == false`): callback forwards BTP capabilities messages to `hsResp` channel, ignores everything else.
- **After handshake** (`dataMode == true`): callback feeds BTP data segments directly to `btp.handleSegment()`.

```go
var dataMode atomic.Bool

c2.EnableNotifications(func(data []byte) {
    slog.Debug("ble: C2 data received", "len", len(data), "hex", hex.EncodeToString(data))
    if isBTPCapabilitiesMessage(data) {
        select {
        case hsResp <- data:
        default:
        }
        return
    }
    if dataMode.Load() {
        if err := btp.handleSegment(data); err != nil {
            slog.Debug("ble: BTP segment error", "err", err)
        }
    }
})

// ... after handshake completes:
dataMode.Store(true)
```

**2. Data poller replaced with disconnect watcher.** The poller's secondary role — detecting silent peripheral disconnections — was preserved in a lightweight goroutine that checks connectivity every 1 second (vs. the old 10 ms poll loop):

```go
func (c *BLEConn) startDisconnectWatcher() {
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-c.closed:
                return
            case <-ticker.C:
                if !c.c2.IsConnected() {
                    slog.Debug("ble: disconnect watcher detected peripheral disconnection, closing connection")
                    c.Close()
                    return
                }
            }
        }
    }()
}
```

### Files changed

- **`internal/transport/ble.go`**
  - Notification callback now calls `btp.handleSegment()` when `dataMode` is set (after handshake).
  - Removed `startC2DataPoller` (10 ms polling loop with data delivery + disconnect detection).
  - Added `startDisconnectWatcher` (1 s ticker, connectivity check only).
  - Updated comments throughout `DialBLE` to reflect the single-path architecture.
  - Added `sync/atomic` import for `atomic.Bool`.

- **`internal/transport/ble_test.go`**
  - Renamed `TestBLEConn_DataPollerDetectsDisconnection` → `TestBLEConn_DisconnectWatcherDetectsDisconnection`.
  - Updated to call `startDisconnectWatcher()` instead of `startC2DataPoller()`.
  - Updated comments.

### Result

Each C2 indication now appears exactly once in the logs:

```
23:49:27 DEBUG  ble: C2 data received  len=31  hex=0d00011a...
```

CPU usage reduced (1 s ticker vs 10 ms poll), log volume halved, and the code is simpler.

---

## 4. BLE Commissioning Failure for Thread Devices — Missing Credential Detection, Wrong Step Ordering, Slow Failure

### Problem

BLE commissioning of a Thread device failed consistently at the AddNOC step. The entire commissioning flow over BLE succeeded up to and including AddTrustedRootCertificate, but when AddNOC was sent (a large 607-byte InvokeRequest fragmented into 3 BTP segments), the device disconnected BLE ~2 seconds later without ever sending a response:

```
00:33:47 DEBUG  ble: C1 write dispatched to bt_queue  bytes=244   ← segment 1
00:33:47 DEBUG  ble: C1 write dispatched to bt_queue  bytes=244   ← segment 2
00:33:47 DEBUG  ble: C1 write dispatched to bt_queue  bytes=128   ← segment 3
00:33:49 DEBUG  ble: disconnect watcher detected peripheral disconnection, closing connection
00:33:49 DEBUG  controller: connection closed, stopping message pump
✗ Commissioning failed: commissioning: adding NOC: invoking AddNOC: interaction: receiving response: context deadline exceeded
```

Three symptoms compounded the failure:
1. The commissioning flow never detected that this was a Thread-only device requiring a `--thread-dataset` flag. It blindly proceeded through all steps and only failed cryptically at AddNOC.
2. Network credentials (Thread operational dataset) would never have been sent even if available, because network setup was ordered **after** AddNOC — and the BLE connection dropped during AddNOC.
3. After the BLE disconnect, the `exchange.Receive` call blocked for **30 seconds** (the `invokeResponseTimeout`) before reporting the error, because the exchange had no way to know the connection was gone.

### Root Cause

Four bugs, all contributing to the failure:

**Bug 1 (root cause): No detection of required network credentials.** The commissioning flow never checked what network interface the device supports. The `--thread-dataset` flag was not provided in the command (`matter commission code 3459-430-8805 --verbose`), so `params.Network` was `nil` and the network setup block was skipped entirely. For a Thread-only device, Thread credentials are mandatory — without them the device has no operational network to join after AddNOC, and commissioning cannot proceed to CASE over IP. The flow should have failed early with a clear error message instead of proceeding to AddNOC and hitting a mysterious BLE disconnect.

**Bug 2: Wrong commissioning step ordering.** Even when credentials *are* provided, the commissioning flow sent AddTrustedRoot and AddNOC (steps 10–11) *before* network setup (steps 12–13). This followed the Matter spec's nominal order but not the practical order used by chip-tool. Thread devices are constrained single-core MCUs (nRF52840, EFR32, etc.) that perform heavy crypto and flash operations during AddNOC (certificate validation, key derivation, fabric state persistence). This can take 1–2 seconds, during which the MCU cannot service BLE connection events, causing the BLE link-layer supervision timeout to expire. The peripheral disconnects at the radio level.

With network credentials sent *after* AddNOC, the BLE disconnect meant the device never received its Thread operational dataset. chip-tool's actual ordering is: network setup → AddTrustedRoot → AddNOC. By delivering the Thread dataset first, the device stores it in flash. Then, even if BLE drops during AddNOC, the device can join Thread using the stored credentials.

**Bug 3: Exchanges not closed on connection loss.** When `runMessagePump` detected `transport.ErrConnClosed` and exited, it stopped dispatching messages to exchanges but did **not close** the exchange channels. Any goroutine blocked in `exchange.Receive` (e.g. waiting for the AddNOC InvokeResponse) continued blocking until its context timed out — 30 seconds by default (`invokeResponseTimeout`). This made the failure unnecessarily slow and confusing.

**Bug 4: No graceful handling of BLE disconnect during AddNOC.** Even with correct ordering and credentials, some Thread devices will still disconnect BLE during AddNOC processing. The commissioning flow treated any AddNOC error as fatal, with no attempt to recover by checking whether the device became reachable on its operational network.

### Fix

**1. Early detection of required network credentials.** After PASE is established and BasicInformation is read, the commissioning flow now reads the `NetworkCommissioning` cluster (0x0031) `FeatureMap` attribute (0xFFFC) on endpoint 0. The FeatureMap bits indicate the device's network interfaces:

| Bit | Meaning |
|-----|---------|
| 0 (0x01) | Wi-Fi |
| 1 (0x02) | Thread |
| 2 (0x04) | Ethernet |

When `params.Network` is nil and the device does not support Ethernet, the flow fails immediately with a targeted error message:

- Thread-only device: `"device requires Thread network credentials\n  Provide a Thread operational dataset with --thread-dataset <hex>"`
- WiFi-only device: `"device requires WiFi network credentials\n  Provide WiFi credentials with --wifi-ssid <ssid> --wifi-password <password>"`
- WiFi+Thread device: message suggests either option

When the device supports Ethernet (possibly alongside other interfaces), no credentials are required and the flow proceeds normally.

**2. Reordered commissioning steps: network setup before AddNOC.** The commissioning flow now matches chip-tool's order:

| Step | Before (broken) | After (correct, matches chip-tool) |
|------|------------------|------------------------------------|
| 10 | AddTrustedRootCertificate | Network setup (AddOrUpdateThreadNetwork) |
| 11 | AddNOC | Network connect (ConnectNetwork) |
| 12 | Network setup | AddTrustedRootCertificate |
| 13 | Network connect | AddNOC |

The `CommissioningStep` enum constants were reordered to reflect this, ensuring progress callbacks report the actual execution order.

**3. `ExchangeManager.CloseAll()` on connection loss.** Added a `CloseAll()` method to `ExchangeManager` that closes every active exchange's incoming channel and removes it from the manager. `runMessagePump` now calls `c.exchanges.CloseAll()` when it exits due to `ErrConnClosed`:

```go
if errors.Is(err, transport.ErrConnClosed) {
    slog.Debug("controller: connection closed, stopping message pump")
    c.exchanges.CloseAll()
    return
}
```

This causes any goroutine blocked in `exchange.Receive` to return immediately with `"protocol: exchange N closed"` instead of waiting 30 seconds.

**4. Optimistic AddNOC recovery.** If AddNOC fails with `transport.ErrConnClosed` or `context.DeadlineExceeded` (both indicate the request was sent but the response was lost due to BLE disconnect), the commissioning flow now proceeds optimistically:

- Logs the BLE disconnect and continues to CASE establishment.
- Uses extended retry parameters for CASE (6 attempts with a 5-second initial wait instead of 3 attempts with 2-second wait), giving Thread devices time to attach to the mesh and start mDNS advertising.
- If CASE succeeds, AddNOC clearly worked and CommissioningComplete is sent over the CASE session as normal.
- If CASE fails after all retries, a descriptive error is returned explaining that BLE disconnected during AddNOC and the device was not reachable on the operational network. The failsafe timer on the device will automatically roll back its state.

### Files changed

- **`internal/commissioning/flow.go`**
  - After reading BasicInformation, reads `NetworkCommissioning` FeatureMap (cluster 0x0031, attribute 0xFFFC) and fails early with a helpful error when network credentials are required but missing.
  - Added `decodeTLVUint32()` helper for parsing the FeatureMap response.
  - Reordered `CommissioningStep` enum: `StepNetworkSetup` and `StepNetworkConnect` now precede `StepAddTrustedRoot` and `StepAddNOC`.
  - Reordered `String()` name table to match.
  - `Commission()`: network setup block moved before AddTrustedRoot/AddNOC, with a detailed comment explaining why this ordering is critical for Thread devices.
  - `Commission()`: AddNOC error handling checks for `transport.ErrConnClosed` / `context.DeadlineExceeded` and sets `bleDisconnectedDuringAddNOC` flag.
  - `Commission()`: CASE retry loop uses extended parameters (6 retries, 5 s initial wait) when the flag is set.
  - Added `"errors"` and `"github.com/p0fi/matter-cli/internal/transport"` imports.

- **`internal/commissioning/flow_test.go`**
  - Enhanced `mockInteractionClient` with per-attribute `readOverrides` map for fine-grained mock responses.
  - Added `encodeTLVUint32()` test helper.
  - Added `TestCommissioner_Commission_ThreadDeviceNoCredentials`: verifies that a Thread-only device (FeatureMap 0x02) without `--thread-dataset` fails early with a message mentioning "Thread network credentials" and "--thread-dataset".
  - Added `TestCommissioner_Commission_WiFiThreadDeviceNoCredentials`: verifies WiFi+Thread device (FeatureMap 0x03) without credentials fails early.
  - Added `TestCommissioner_Commission_EthernetDeviceNoCredentials`: verifies Ethernet-only device (FeatureMap 0x04) commissions successfully without network credentials.
  - Added `TestCommissioner_Commission_ThreadDeviceWithCredentials`: verifies Thread-only device commissions successfully when Thread credentials are provided.

- **`internal/protocol/exchange.go`**
  - Added `CloseAll()` method on `ExchangeManager`: iterates all exchanges, closes each, and removes it from the map.

- **`internal/controller/controller.go`**
  - `runMessagePump`: calls `c.exchanges.CloseAll()` when exiting due to `ErrConnClosed`, so blocked `Receive` calls unblock immediately.

### Result

For the original failing command (`matter commission code 3459-430-8805 --verbose` with a Thread device):

```
✗ Commissioning failed: commissioning: device requires Thread network credentials
  Provide a Thread operational dataset with --thread-dataset <hex>
```

The error now appears within seconds of PASE establishment — before any certificates are exchanged — with a clear message explaining what flag to add. No more 30-second hang, no more cryptic "context deadline exceeded".

When the correct flag is provided (`matter commission code 3459-430-8805 --thread-dataset <hex>`), the flow:

1. Detects Thread support via FeatureMap — credentials are present, so no error.
2. Delivers Thread credentials over BLE **before** AddNOC.
3. If BLE drops during AddNOC, the device joins Thread using the stored credentials.
4. The commissioner discovers the device on the operational network within seconds.
5. CASE and CommissioningComplete succeed over Thread/IP.
6. If BLE does drop, the error feedback is immediate (not 30 seconds delayed).

---

## 5. Optimistic AddNOC Recovery Not Triggering — `ErrExchangeClosed` Not Matched

### Problem

Even after the fixes in section 4, BLE commissioning of a Thread device with `--thread-dataset` still failed fatally at AddNOC instead of recovering optimistically:

```
✗ Commissioning failed: commissioning: adding NOC: invoking AddNOC: interaction: receiving response: protocol: exchange 10 closed
```

The network credentials were delivered successfully (steps 10–11 completed), and the BLE disconnect was detected immediately (no 30-second hang), but the commissioning flow treated the AddNOC failure as fatal rather than proceeding to operational discovery via CASE.

### Root Cause

The optimistic recovery logic in `Commission()` checked for two error types:

```go
if errors.Is(addNOCErr, transport.ErrConnClosed) || errors.Is(addNOCErr, context.DeadlineExceeded) {
    // proceed optimistically...
}
```

But when the BLE connection dropped, `ExchangeManager.CloseAll()` closed all exchange channels, causing `Exchange.Receive()` to return a plain `fmt.Errorf("protocol: exchange %d closed", e.ID)` — a bare string error with no sentinel. This error did not wrap `transport.ErrConnClosed` or `context.DeadlineExceeded`, so `errors.Is()` returned false for both checks, and the flow took the fatal error path.

The error propagation chain was:
```
"commissioning: adding NOC: invoking AddNOC: interaction: receiving response: protocol: exchange 10 closed"
```

None of the wrappers in this chain included a matchable sentinel error. The `CloseAll()` fix from section 4 correctly unblocked the exchange (no 30 s hang), but the *type* of the resulting error was not recognized by the recovery logic.

### Fix

**1. Introduced `protocol.ErrExchangeClosed` sentinel error.** Added a package-level sentinel in `internal/protocol/exchange.go`:

```go
var ErrExchangeClosed = errors.New("exchange closed")
```

**2. Used the sentinel in `Exchange.Receive()` and `ExchangeManager.HandleMessage()`.** Both sites that previously used bare `fmt.Errorf` now wrap the sentinel with `%w`:

```go
// Exchange.Receive — when channel is closed
return nil, fmt.Errorf("protocol: exchange %d: %w", e.ID, ErrExchangeClosed)

// ExchangeManager.HandleMessage — when dispatching to a closed exchange
return fmt.Errorf("protocol: exchange %d: %w", e.ID, ErrExchangeClosed)
```

**3. Added `protocol.ErrExchangeClosed` to the optimistic recovery check.** The AddNOC error handler now matches all three conditions:

```go
if errors.Is(addNOCErr, transport.ErrConnClosed) ||
    errors.Is(addNOCErr, context.DeadlineExceeded) ||
    errors.Is(addNOCErr, protocol.ErrExchangeClosed) {
    // proceed optimistically...
}
```

### Files changed

- **`internal/protocol/exchange.go`**
  - Added `ErrExchangeClosed` sentinel error (`var ErrExchangeClosed = errors.New("exchange closed")`).
  - Added `"errors"` import.
  - `Exchange.Receive()`: changed bare `fmt.Errorf` to wrap `ErrExchangeClosed` with `%w`.
  - `ExchangeManager.HandleMessage()`: changed bare `fmt.Errorf` to wrap `ErrExchangeClosed` with `%w`.

- **`internal/commissioning/flow.go`**
  - Added `"github.com/p0fi/matter-cli/internal/protocol"` import.
  - AddNOC error check: added `errors.Is(addNOCErr, protocol.ErrExchangeClosed)` to the optimistic recovery condition.

### Result

With the fix, the same command that previously failed fatally now recovers:

```
$ matter commission code 3459-430-8805 --verbose --thread-dataset 8d0bc474b224d85c31021d53e404fac4
● Commissioning device with code 3459-430-8805 as node 5
...
● Adding NOC
  DEBUG  commissioning: BLE disconnected during AddNOC, proceeding optimistically  err="...protocol: exchange 10: exchange closed"
  DEBUG  commissioning: BLE dropped during AddNOC, using extended CASE retry window  retries=6  initialWait=5s
● Establishing CASE session
  DEBUG  commissioning: CASE attempt failed  attempt=1  ...
  DEBUG  commissioning: retrying CASE  attempt=2
● Completing commissioning
✓ Device commissioned successfully as node 5
```

The flow detects the exchange closure as a BLE disconnect, proceeds optimistically to operational discovery, retries CASE with extended timeouts while the Thread device attaches to the mesh, and completes commissioning over the IP network.

---

## 6. CASE Over Thread Fails — Wrong mDNS Device Selected and IPv6 Address Parse Error

### Problem

After the optimistic AddNOC recovery (section 5) started working, BLE commissioning of a Thread device progressed past AddNOC but failed during CASE establishment. Two distinct failures appeared across the 6 CASE retry attempts:

**Attempts 1–5:** CASE connected to the wrong device. The mDNS discovery found an operational entry `8E9462691A5722B9-0000000000000004` at `192.168.1.66:5540` — a previously commissioned device (node 4), not the device being commissioned (node 5). CASE was rejected with `StatusReport general=0x0001 protocol=0x00000000 code=0x0001` (Sigma2 rejected) because the device's fabric credentials didn't match the newly issued NOC.

**Attempt 6:** mDNS finally discovered the correct device (`09E31EF160CD9AB3-000000001BFCB968`) but at a Thread mesh-local IPv6 address. The address was formatted as `fd48:8115:9eb9:0:8c37:6419:bdc4:9ee1:5540`, which `net.ResolveUDPAddr` rejected with `"too many colons in address"` because IPv6 addresses with ports require bracket notation (`[addr]:port`).

```
22:53:07 DEBUG  ble: found operational device via mDNS  name=8E9462691A5722B9-0000000000000004  addr=192.168.1.66:5540
22:53:07 DEBUG  commissioning: CASE attempt failed  attempt=1  err=...CASE Sigma2 rejected...
...
22:54:13 DEBUG  ble: found operational device via mDNS  name=09E31EF160CD9AB3-000000001BFCB968  addr=fd48:8115:9eb9:0:8c37:6419:bdc4:9ee1:5540
22:54:13 DEBUG  commissioning: CASE attempt failed  attempt=6  err=...too many colons in address
✗ Commissioning failed: commissioning: BLE disconnected during AddNOC and device not reachable on operational network: ...too many colons in address
```

### Root Cause

Two bugs in `internal/controller/controller_ble.go`, method `bleSessionEstablisher.EstablishCASE`:

**Bug 1: No mDNS filtering by node ID.** The operational discovery loop picked the first mDNS entry with any IP address, regardless of which device it belonged to. The Matter operational mDNS instance name has the format `<compressed-fabric-id-hex>-<node-id-hex>` (e.g. `8E9462691A5722B9-0000000000000005` for node 5). The code should have matched on the node ID suffix to avoid connecting to stale advertisements from previously commissioned devices.

```go
// Before: picks the first device with ANY IP, regardless of node ID
for _, dev := range devices {
    if len(dev.IPs) > 0 {
        ipAddr = fmt.Sprintf("%s:%d", dev.IPs[0], dev.Port)
        break
    }
}
```

**Bug 2: IPv6 addresses formatted without brackets.** `fmt.Sprintf("%s:%d", dev.IPs[0], dev.Port)` produces `fd48:…:9ee1:5540` for an IPv6 address, which is ambiguous — Go's `net.ResolveUDPAddr` cannot distinguish the port from the address octets. The correct format is `[fd48:…:9ee1]:5540`. Go's `net.JoinHostPort` handles this automatically.

The same IPv6 formatting bug also existed in `internal/controller/adapters.go` in the `controllerDiscoverer.DiscoverCommissionable` method (used for IP-based commissioning discovery), though it was less likely to trigger since most commissionable devices are on local IPv4 networks.

### Fix

**1. Filter mDNS results by node ID.** The operational mDNS instance name encodes the node ID as the last 16 hex characters after the `-` separator. The discovery loop now computes the expected suffix (`-<016X nodeID>`) and skips entries that don't match, logging skipped entries for debugging:

```go
nodeIDSuffix := fmt.Sprintf("-%016X", nodeID)
for _, dev := range devices {
    if len(dev.IPs) == 0 {
        continue
    }
    if !strings.HasSuffix(strings.ToUpper(dev.Name), nodeIDSuffix) {
        slog.Debug("ble: skipping mDNS entry (node ID mismatch)",
            "name", dev.Name, "want", nodeIDSuffix)
        continue
    }
    ipAddr = net.JoinHostPort(dev.IPs[0].String(), fmt.Sprintf("%d", dev.Port))
    break
}
```

**2. Use `net.JoinHostPort` everywhere.** Replaced all `fmt.Sprintf("%s:%d", ip, port)` patterns with `net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port))`, which automatically wraps IPv6 addresses in brackets (e.g. `[fd48:…:9ee1]:5540`) and leaves IPv4 addresses unchanged (e.g. `192.168.1.66:5540`).

### Files changed

- **`internal/controller/controller_ble.go`**
  - Added `"net"` import.
  - `bleSessionEstablisher.EstablishCASE()`: mDNS result loop now computes `nodeIDSuffix` from `nodeID` and skips entries whose instance name doesn't end with the suffix (case-insensitive comparison).
  - `bleSessionEstablisher.EstablishCASE()`: replaced `fmt.Sprintf("%s:%d", ...)` with `net.JoinHostPort(...)` for IPv6-safe address formatting.
  - Error message for no-device-found now includes the node ID being searched for.

- **`internal/controller/adapters.go`**
  - Added `"net"` import.
  - `controllerDiscoverer.DiscoverCommissionable()`: both address formatting sites replaced with `net.JoinHostPort(...)`.

### Result

With both fixes, the CASE retry loop:

1. Skips stale mDNS entries from other nodes (e.g. node 4) with a debug log explaining the skip.
2. Only connects to the entry matching the commissioned node ID (node 5).
3. Correctly formats Thread IPv6 addresses with brackets, so `net.ResolveUDPAddr` parses them successfully.

---

## 7. Thread ConnectNetwork Sends Wrong Network ID — First 8 Bytes Instead of Extended PAN ID

### Problem

BLE commissioning of a Thread device completes all steps up through AddNOC, but the device never appears on the operational network via mDNS. After AddNOC the BLE connection drops (expected for Thread devices doing heavy crypto), and 6 CASE retry attempts over ~75 seconds all fail to find the device on `_matter._tcp`.

The commissioning flow sends `ConnectNetwork` (cluster 0x0031, command 0x06) before AddNOC, and the device returns a response — but the device never actually joins the Thread mesh.

### Root Cause

The `connectNetwork` function in `flow.go` used `dataset[:8]` — the first 8 raw bytes of the Thread operational dataset — as the network ID for the `ConnectNetwork` command:

```go
// WRONG: first 8 bytes of the raw dataset
networkID = creds.Thread.OperationalDataset[:8]
```

For the actual dataset `000300001901028cef0208ed47ad8b290344c6...`, the first 8 bytes are `000300001901028c`, which is the Channel TLV header (`00 03 000019`) and start of the PAN ID TLV (`01 02 8c`). This is garbage — not a valid network identifier.

The Matter spec requires the network ID for Thread's `ConnectNetwork` to be the **Extended PAN ID** (8 bytes), which lives at TLV type `0x02` inside the Thread operational dataset. For this dataset, the correct value is `ed47ad8b290344c6`.

The Thread operational dataset uses a simple TLV format (1-byte type, 1-byte length, variable value) defined by the Thread specification:

| Offset | Type | Length | Value | Meaning |
|--------|------|--------|-------|---------|
| 0 | 0x00 | 3 | 000019 | Channel (page=0, channel=25) |
| 5 | 0x01 | 2 | 8cef | PAN ID |
| 9 | 0x02 | 8 | ed47ad8b290344c6 | **Extended PAN ID** ← this is what ConnectNetwork needs |
| 19 | 0x03 | 8 | 4d79486f6d653634 | Network Name ("MyHome64") |
| 29 | 0x04 | 16 | 28faa6e0...5fc6 | PSKc |
| 47 | 0x05 | 16 | 8d0bc474...fac4 | Network Key |
| ... | ... | ... | ... | ... |

Because the device received a `ConnectNetwork` for a network ID it didn't recognize, it never joined the Thread mesh. Then when BLE dropped during AddNOC, the device had no network to advertise on — explaining why it never appeared on `_matter._tcp`.

### Fix

1. **Added `ExtractExtendedPANID` function** (`internal/commissioning/network.go`) that properly parses the Thread dataset TLV structure and extracts the 8-byte Extended PAN ID (type `0x02`). Returns an error if the field is missing or truncated.

2. **Fixed `connectNetwork`** (`internal/commissioning/flow.go`) to call `ExtractExtendedPANID()` instead of slicing `dataset[:8]`:

```go
// CORRECT: extract Extended PAN ID from Thread TLV structure
extPanID, err := ExtractExtendedPANID(creds.Thread.OperationalDataset)
if err != nil {
    return fmt.Errorf("extracting Extended PAN ID for ConnectNetwork: %w", err)
}
networkID = extPanID
```

3. **Added network command response validation** — `AddOrUpdateThreadNetwork`, `AddOrUpdateWiFiNetwork`, and `ConnectNetwork` responses are now parsed for `NetworkingStatus`. Previously the response data was silently discarded, so errors like `NetworkNotFound` (status 5) would go undetected. The new `checkNetworkResponse` function parses the response TLV and returns a descriptive error if the status is non-zero.

4. **Added `ConnectNetwork` debug logging** showing the exact network ID bytes and network type being sent.

### Files changed

| File | Change |
|------|--------|
| `internal/commissioning/network.go` | Added `ExtractExtendedPANID()` function |
| `internal/commissioning/network_test.go` | Added tests for `ExtractExtendedPANID` including the real dataset from the failing log |
| `internal/commissioning/flow.go` | Fixed `connectNetwork()` to use `ExtractExtendedPANID()`; added `checkNetworkResponse()` and `networkingStatusName()`; added response validation to `setupNetwork()` and `connectNetwork()`; added debug logging for ConnectNetwork |

### Result

After the fix, the `ConnectNetwork` command sends the correct Extended PAN ID (`ed47ad8b290344c6`) as the network ID. Confirmed via the TLV payload in the debug log:

```
payloadHex=...300008ed47ad8b290344c6...
```

However, the device still fails to appear on the operational mDNS network after AddNOC. This led to the investigation in section 8.

---

## 8. CASE Discovery After BLE Commissioning — Wrong mDNS Matching and Missing Diagnostics

### Problem

Even after the ConnectNetwork fix (section 7), the Thread device does not appear on `_matter._tcp` mDNS with the expected instance name. Six CASE retry attempts over ~75 seconds all fail. However, a *new* mDNS entry (`E44DA137FE34C60C-02A2609B8AC05B21`) appeared mid-test that was not present before commissioning started — suggesting the device DID join the Thread mesh and register on the operational network, but with an unexpected instance name.

The mDNS matching code only matched on the node ID suffix (`-0000000000000005`), which was correct per the spec, but provided no diagnostic information about what entries were actually discovered or what our own compressed fabric ID was — making it impossible to determine if the new entry was our device.

### Root Cause

Three compounding issues:

1. **Compressed fabric ID not stored**: The compressed fabric ID was computed during fabric initialization (via `crypto.CompressedFabricID()`) but only used for IPK derivation and then discarded. It was not available for mDNS instance name matching.

2. **Suffix-only matching**: The mDNS matching used `strings.HasSuffix(name, "-0000000000000005")` which only checked the node ID portion of the instance name. Without knowing our compressed fabric ID, we couldn't determine which of the discovered entries belonged to our fabric.

3. **Insufficient diagnostic logging**: Only mismatched entries were logged (with `"want"` showing the suffix). There was no logging of:
   - Our expected full instance name (compressed fabric ID + node ID)
   - All discovered entries with their IP addresses
   - Whether any entry matched our compressed fabric ID with a different node ID

### Fix

1. **Stored compressed fabric ID on `fabricIdentity`** (`internal/controller/fabric.go`) — Added `compressedFabricID []byte` field to the `fabricIdentity` struct. Both `loadFabric()` and `createFabric()` now store the computed compressed fabric ID.

2. **Full instance name matching** (`internal/controller/controller_ble.go`) — The mDNS matching in `EstablishCASE` now builds the expected full instance name (`<compressedFabricID>-<nodeID>`, e.g. `A1B2C3D4E5F6A7B8-0000000000000005`) and matches on the full string when the compressed fabric ID is available. Falls back to node-ID-suffix matching if the fabric isn't loaded.

3. **Comprehensive diagnostic logging** — Before filtering, ALL discovered `_matter._tcp` entries are logged with their full details:

```
ble: mDNS operational entry  index=0  name=E44DA137FE34C60C-02A2609B8AC05B21
    host=device.local.  port=5540  ips=fd48:8115:9eb9:0:1234:5678:abcd:ef01
```

And the expected instance name is logged at discovery start:

```
ble: discovering device on operational network for CASE  nodeID=5
    expectedInstanceName=A1B2C3D4E5F6A7B8-0000000000000005
    compressedFabricID=A1B2C3D4E5F6A7B8
```

### Files changed

| File | Change |
|------|--------|
| `internal/controller/fabric.go` | Added `compressedFabricID` field to `fabricIdentity`; stored it in both `loadFabric()` and `createFabric()` |
| `internal/controller/controller_ble.go` | Rewrote mDNS matching to use full instance name; added comprehensive entry logging; improved error message to include expected instance name |

### Expected Result

The next commissioning attempt will reveal:

1. **Our compressed fabric ID** — logged at the start of mDNS discovery
2. **Whether the device appeared on mDNS at all** — all entries logged with IPs
3. **Whether the new entry (`E44DA137FE34C60C-02A2609B8AC05B21`) is our device** — if `E44DA137FE34C60C` matches our compressed fabric ID, then the device is registering with a scrambled node ID (`02A2609B8AC05B21` instead of `0000000000000005`), pointing to an issue in NOC encoding or AddNOC processing
4. **Whether ConnectNetwork actually succeeded** — the new response validation will log the `NetworkingStatus` or fail fast with a descriptive error

### Open questions for next debugging session

- Is the AddNOC message fully delivered before BLE disconnects? The third BTP fragment is dispatched but "peripheral not ready" deferrals suggest the BLE radio may be congested.
- Does the device process AddNOC successfully when BLE drops mid-transfer? If not, the failsafe timer should roll back, but the device may still have Thread credentials stored.
- Is the mysterious `E44DA137FE34C60C-02A2609B8AC05B21` entry our device on our fabric, or a coincidence from another controller?

---

## 9. ALL Network Provisioning Was Before AddNOC — Must Be After Per chip-tool Reference

### Problem (first attempt)

After the fixes in sections 7 and 8, BLE commissioning of a Thread device would invoke `ConnectNetwork` before `AddNOC`. The device spent 30-60 s trying to join Thread while on BLE, exhausting the BLE supervision timeout. The BLE connection died before AddNOC was ever sent:

```
commissioning: connecting network: invoking ConnectNetwork: interaction: receiving response: protocol: exchange 8: exchange closed
```

The initial fix (9a) moved only `ConnectNetwork` after AddNOC while keeping `AddOrUpdateThreadNetwork` before it. This eliminated the 60 s hang — AddNOC was reached and BLE dropped during AddNOC as expected for constrained Thread devices. But the device still never appeared on `_matter._tcp`:

```
ble: mDNS operational entry  index=0  name=8E9462691A5722B9-0000000000000004  ...  ← node 4, a different device
ble: mDNS operational entry  index=1  name=E44DA137FE34C60C-02A2609B8AC05B21  ...  ← different fabric
ble: skipping mDNS entry (name mismatch)  name=8E9462691A5722B9-0000000000000004  want=8E9462691A5722B9-0000000000000005
```

Our compressed fabric ID (`8E9462691A5722B9`) was visible on another node (node 4), but node 5 (the Thread device being commissioned) never appeared across 6 CASE retries over ~80 seconds.

### Problem (second attempt / current analysis)

The critical clue is in the AddNOC BLE write sequence:

```
ble: C1 write dispatched to bt_queue  bytes=244
ble: C1 write deferred: peripheral not ready (canSendWriteWithoutResponse=false)
ble: C1 write dispatched to bt_queue  bytes=244
ble: C1 write deferred: peripheral not ready (canSendWriteWithoutResponse=false)
ble: C1 write dispatched to bt_queue  bytes=129
ble: disconnect watcher detected peripheral disconnection, closing connection
```

The AddNOC invoke payload is 608 bytes, split into 3 BTP fragments (244 + 244 + 129). Each fragment was eventually dispatched to `bt_queue`, but 2 of 3 were initially deferred because the peripheral wasn't ready. BLE disconnected immediately after the last fragment was dispatched — there's no guarantee all fragments were received by the device.

Even if AddNOC was received and processed, network credentials were sent via `AddOrUpdateThreadNetwork` (exchange 7) but `ConnectNetwork` was skipped because BLE had already dropped. The device has the NOC and stored Thread credentials, but was **never told to connect**. Without a `ConnectNetwork` command, the device does not auto-join the Thread mesh — it just sits there with stored-but-inactive credentials.

### Root Cause

Verified against `connectedhomeip/src/controller/AutoCommissioner.cpp`, the chip-tool commissioning state machine order is:

```
kSendTrustedRootCert → kSendNOC → [kICDRegistration] →
kThreadNetworkSetup → kFailsafeBeforeThreadEnable → kThreadNetworkEnable →
kEvictPreviousCaseSessions → kFindOperational → kSendComplete
```

Mapped to commands:
1. `AddTrustedRootCertificate`
2. **`AddNOC`**
3. **`AddOrUpdateThreadNetwork`** (= kThreadNetworkSetup)
4. **`ConnectNetwork`** (= kThreadNetworkEnable)
5. Find operational device via mDNS
6. `CommissioningComplete` over CASE

**ALL network provisioning — both `AddOrUpdateThreadNetwork` and `ConnectNetwork` — happens AFTER `AddNOC`**, not before. Our code had `AddOrUpdateThreadNetwork` before AddNOC, which meant:

- When BLE dropped during AddNOC (common on constrained Thread MCUs), network credentials were already stored but `ConnectNetwork` was never sent.
- The device had its NOC (maybe — BLE may have dropped mid-transfer) and Thread credentials stored, but was never told to join the network.
- Without a `ConnectNetwork`, the device sits idle and never advertises on `_matter._tcp`.

By moving ALL network provisioning after AddNOC (matching chip-tool), the flow becomes:
1. AddNOC completes (or BLE drops — handled optimistically)
2. If BLE survived: send `AddOrUpdateThreadNetwork` + `ConnectNetwork`
3. If BLE drops during either step: proceed optimistically — the device is transitioning to Thread, which is exactly the expected behavior
4. Discover device on `_matter._tcp` via mDNS
5. CASE + CommissioningComplete

This means that if BLE drops during AddNOC, we **cannot** deliver network credentials at all. But this is the same situation chip-tool faces — and in practice, for Thread devices that already have a Thread network stored from a previous attempt (or from a factory default), the device may still be reachable. If not, the failsafe rolls back and the user retries.

### Fix

1. **Moved ALL network provisioning after AddNOC** — both `AddOrUpdateThreadNetwork` and `ConnectNetwork` now happen after `AddNOC`, matching the chip-tool reference implementation exactly.

2. **Reordered step constants** to match: `StepAddTrustedRoot` → `StepAddNOC` → `StepNetworkSetup` → `StepNetworkConnect` → `StepEstablishCASE` → `StepCommissioningComplete`.

3. **Handle BLE drops during any network step** — `AddOrUpdateThreadNetwork` and `ConnectNetwork` both treat `ErrConnClosed` / `ErrExchangeClosed` as optimistic success, same as AddNOC.

4. **If BLE dropped during AddNOC**, skip network provisioning entirely and log it — the device has its NOC but no network credentials were delivered. CASE discovery will still be attempted in case the device is reachable on a previously configured network.

5. **Increased CASE retry window** — 8 retries with 8 s initial wait and 5 s between retries (~43 s window) to give Thread devices more time to join the mesh after ConnectNetwork.

Updated flow:

```
Step 10: AddTrustedRootCertificate
Step 11: AddNOC                          ← may cause BLE drop (optimistic)
Step 12: AddOrUpdateThreadNetwork        ← skipped if BLE dropped, may itself cause BLE drop
Step 13: ConnectNetwork                  ← skipped if BLE dropped, may itself cause BLE drop  
Step 14: CASE over mDNS _matter._tcp
Step 15: CommissioningComplete over CASE
```

### Files changed

| File | Change |
|------|--------|
| `internal/commissioning/flow.go` | Moved `setupNetwork` + `connectNetwork` to after `addNOC`; reordered step constants to `StepAddTrustedRoot` → `StepAddNOC` → `StepNetworkSetup` → `StepNetworkConnect` → `StepEstablishCASE` → `StepCommissioningComplete`; BLE-drop handling for network steps; increased CASE retries to 8 with 8 s initial wait |

### Expected Result

The commissioning flow now matches the chip-tool reference implementation (`AutoCommissioner.cpp`) exactly:

1. `AddTrustedRootCertificate` — fast ✓
2. `AddNOC` — may cause BLE drop, handled optimistically ✓
3. `AddOrUpdateThreadNetwork` — if BLE alive; stores credentials ✓
4. `ConnectNetwork` — if BLE alive; device joins Thread (may cause BLE drop) ✓
5. CASE discovery over mDNS `_matter._tcp` with full instance name matching ✓
6. `CommissioningComplete` over CASE ✓

The key insight is that chip-tool does NOT store network credentials before AddNOC. Both `AddOrUpdateThreadNetwork` and `ConnectNetwork` are sent after the NOC is installed. This means the device processes AddNOC first (without the BLE radio being saturated by a Thread join), and then network provisioning follows.

### Open questions for next debugging session

- If BLE drops during AddNOC (before network credentials are sent), the commissioning attempt cannot succeed — the device has no network to join. Should we detect this and tell the user to retry, rather than waiting through 8 CASE attempts?
- The `canSendWriteWithoutResponse=false` deferrals during AddNOC fragment delivery suggest the BLE radio is congested. Could increasing the BTP window size or adding a small inter-fragment delay help the last fragment land before the supervision timeout?
- Is node 4 (`8E9462691A5722B9-0000000000000004`) a previously commissioned device on our fabric, confirming the compressed fabric ID computation is correct?