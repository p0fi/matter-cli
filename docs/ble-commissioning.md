# BLE Commissioning — Implementation Plan

> Status: **Planned** — not yet implemented.  
> Last updated: 2026  
> Author: AI planning session

---

## 1. Overview

Matter devices that set the `DiscoveryBLE` bit in their QR-code / manual pairing code setup payload can be commissioned over Bluetooth Low Energy. The existing codebase is well-architected for this — the `transport.Conn` interface, the dependency-injected `Commissioner`, and the transport-agnostic commissioning flow mean BLE support can be layered in without touching the core protocol or commissioning logic.

BLE commissioning uses the **BLE Transport Protocol (BTP)**, defined in Matter spec Chapter 4.15. BTP provides reliable, ordered delivery of Matter messages over two GATT characteristics, with its own segmentation/reassembly and handshake.

**Spec references:**
- Matter Specification §4.15 — BLE Transport Protocol (BTP)
- Matter Specification §5.4.2.5.6 — BLE Advertisement Format
- C++ reference: `connectedhomeip/src/ble/` (`BleLayer`, `BtpEngine`, `BLEEndPoint`)

---

## 2. Architecture

```
┌─────────────────────────────────────────────────┐
│              Commissioner / CLI                  │
├─────────────────────────────────────────────────┤
│    commissioning.Commissioner (unchanged)        │
│    SessionEstablisher (PASE/CASE)                │
│    InteractionClient (IM Read/Write/Invoke)      │
├─────────────────────────────────────────────────┤
│    protocol.ExchangeManager (unchanged)          │
│    protocol.Codec (unchanged)                    │
├────────────────────┬────────────────────────────┤
│  transport.UDPConn │  transport.BLEConn  (NEW)  │
│    (UDP/IP)        │    ┌─────────────────┐     │
│                    │    │  BTP Session     │     │
│                    │    │  (segment/reasm) │     │
│                    │    ├─────────────────┤     │
│                    │    │  GATT C1/C2     │     │
│                    │    │  (write/indicate)│     │
│                    │    └─────────────────┘     │
└────────────────────┴────────────────────────────┘
```

The key insight: **BTP makes BLE look like a datagram transport.** Once BTP is established, `BLEConn` implements `transport.Conn` and the entire stack above it (exchanges, sessions, PASE, IM) works identically to UDP — zero changes required to the protocol, secure, or interaction layers.

### Commissioning flow comparison

```
Current (IP) flow:
  1. Parse QR code → discriminator, passcode
  2. mDNS discover → IP:port
  3. PASE over UDP → secure session
  4. Commission steps (arm failsafe, attest, CSR, NOC, network …)
  5. CASE over UDP/IP

BLE commissioning flow:
  1. Parse QR code → discriminator, passcode, DiscoveryBLE bit
  2. BLE scan → BLE address
  3. PASE over BLE/BTP → secure session
  4. Commission steps (identical — same code, different transport)
  5. Network provisioning → device joins WiFi or Thread
  6. Close BLE connection (per spec: close after CommissioningComplete over CASE)
  7. Discover device on IP network (mDNS _matter._tcp)
  8. CASE over UDP/IP
```

The only structural difference:
- **Steps 2–3**: BLE discovery + BLE transport instead of mDNS + UDP.
- **Step 6**: Explicit BLE teardown after CASE CommissioningComplete (per spec §5.5).
- **Step 7–8**: CASE always transitions to IP — BLE is never used for CASE.

---

## 3. Decisions

| # | Question | Decision |
|---|----------|----------|
| 1 | Auto-detect vs explicit BLE? | Auto-detect: parse `DiscoveryBLE` from QR code and prefer BLE when advertised. Add `--transport ble\|ip\|auto` flag (default: `auto`). |
| 2 | When to close BLE connection? | Close BLE after CommissioningComplete is confirmed over CASE, exactly as the spec requires. |
| 3 | Build tag for BLE? | Default-include BLE. Use `//go:build !noble` to exclude (e.g. for CI servers without Bluetooth hardware). |
| 4 | BLE library? | `tinygo.org/x/bluetooth` — cross-platform Central support: CoreBluetooth on macOS (CGo, system framework only), BlueZ D-Bus on Linux (pure Go). No third-party C++ deps. |

---

## 4. New Files

### 4.1 Transport layer

```
internal/transport/
├── btp.go               # BTP segment encode/decode, session state, handshake
├── btp_test.go          # Pure byte tests — no hardware required
├── ble.go               # BLEConn (transport.Conn impl), DialBLE(), BLEAddr
├── ble_test.go          # Unit tests with mock GATT adapter
├── ble_scanner.go       # BLE scan → list of commissionable devices
├── ble_scanner_test.go  # Scanner tests with mock adapter
└── ble_disabled.go      # Build-tag stub (//go:build noble)
```

### 4.2 Discovery layer

```
internal/discovery/
├── ble.go               # BLEBrowser — implements commissioning.DeviceDiscoverer
└── ble_test.go          # Tests with mock scan results
```

`internal/discovery/device.go` gains a `TransportType` field (`"ip"` or `"ble"`).

### 4.3 Controller layer

```
internal/controller/
└── controller_ble.go    # ConnectPASEoverBLE(), bleSessionEstablisher adapter
```

`internal/controller/adapters.go` gains `bleSessionEstablisher`.

### 4.4 CLI layer

```
cli/
├── commission.go        # Add `matter commission ble` subcommand; --transport flag on `code`
└── discover.go          # Add `matter discover ble` subcommand
```

---

## 5. Matter BLE Service UUIDs

| Role | UUID | Properties |
|------|------|------------|
| Service | `0000FFF6-0000-1000-8000-00805F9B34FB` | — |
| C1 (commissioner → device) | `18EE2EF5-263D-4559-959F-4F9C429F9D11` | Write Without Response |
| C2 (device → commissioner) | `18EE2EF5-263D-4559-959F-4F9C429F9D12` | Indicate |
| C3 (additional data, optional) | `64630238-8772-45F2-B87D-748A83218F04` | Read |

---

## 6. BLE Advertisement Format

Matter devices advertise service data on UUID `0xFFF6`. The service data payload layout (spec §5.4.2.5.6):

```
Byte 0:     OpCode (0x00 = commissionable advertisement)
Byte 1–2:   Discriminator + version (uint16 LE)
              Bits [11:0]  = 12-bit discriminator
              Bits [15:12] = version nibble (0)
Byte 3–4:   Vendor ID  (uint16 LE)
Byte 5–6:   Product ID (uint16 LE)
Byte 7:     Additional Data flag (bit 0 set = C3 contains extra data)
```

---

## 7. BTP Protocol Details

### 7.1 Handshake

```
Commissioner                          Device
    |                                    |
    |-- [C1 Write] BTP Handshake Req --->|
    |    - Supported versions bitmask    |
    |      (bit N = version N supported) |
    |    - Supported ATT MTU (uint16 LE) |
    |    - Window size (uint8)           |
    |                                    |
    |<- [C2 Indicate] BTP Handshake Res -|
    |    - Selected version (uint8)      |
    |    - Selected ATT MTU (uint16 LE)  |
    |    - Window size (uint8)           |
    |                                    |
    [BTP session established]
```

After the handshake the negotiated **segment size** is `ATT_MTU - 3` (3 bytes GATT overhead).

### 7.2 Segment format

```
Byte 0:     BTP Header Flags
              Bit 0: H — set only in handshake messages
              Bit 1: M — management opcode follows (reserved, must be 0)
              Bit 2: A — Ack Number byte is present
              Bit 3: E — End segment (last segment of the message)
              Bit 4: B — Beginning segment (first segment of the message)
[Byte 1]:   Ack Number (uint8) — present when A=1
[Next byte]: Sequence Number (uint8) — present when B=1
[Next 2]:   Message Length (uint16 LE) — present when B=1, total message bytes
[Rest]:     Segment payload (variable length)
```

Rules:
- Every segment carries a sequence number when `B=1`.
- Every segment piggybbacks an ack (`A=1`) when the receiver has outstanding unacknowledged segments to report.
- A single-segment message has both `B=1` and `E=1`.
- The receiver must send a standalone ack segment (empty payload, `A=1`) if it has not piggybacked an ack within the BTP ack timeout (15 s default).

### 7.3 BTP parameters

| Parameter | Default | Range |
|-----------|---------|-------|
| BTP version | 4 | 3–4 |
| Segment size | ATT_MTU − 3 bytes | 20–244 |
| Window size | 6 | 1–255 |
| Ack timeout | 15 s | — |
| Max retries | 4 | — |

### 7.4 Flow control

BTP uses a simple window: the sender may not have more than `window_size` unacknowledged segments outstanding. The sequence number wraps at 255 (uint8).

---

## 8. Detailed Implementation Notes

### 8.1 `internal/transport/btp.go`

Key types and functions to implement:

```go
// btpSession manages a single BTP connection's state machine.
type btpSession struct {
    // Negotiated after handshake
    segmentSize uint16
    version     uint8
    windowSize  uint8

    // Outgoing
    localSeq    uint8           // next segment sequence number to send
    unackedSent uint8           // count of sent but unacked segments
    pendingAck  uint8           // peer's seq we need to ack

    // Incoming reassembly
    rxBuf       bytes.Buffer
    rxExpected  uint16          // total message length from B segment
    rxActive    bool

    // Completed inbound messages, buffered for BLEConn.Receive()
    messages    chan []byte
}

// segment splits a complete Matter message into BTP-framed []byte segments
// ready for writing to C1.
func (s *btpSession) segment(msg []byte) [][]byte

// handleSegment processes one incoming BTP segment from C2.
// On reassembly completion it sends the full message to s.messages.
func (s *btpSession) handleSegment(data []byte) error

// btpHandshakeRequest builds the 6-byte BTP HandshakeRequest payload.
func btpHandshakeRequest(supportedVersions uint8, mtu uint16, windowSize uint8) []byte

// parseBTPHandshakeResponse parses the HandshakeResponse from C2.
func parseBTPHandshakeResponse(data []byte) (version uint8, mtu uint16, windowSize uint8, err error)
```

All BTP logic is pure byte manipulation — fully unit-testable without any BLE hardware.

### 8.2 `internal/transport/ble_scanner.go`

```go
// BLEScanner scans for Matter devices advertising on UUID 0xFFF6.
type BLEScanner struct {
    adapter bleAdapter  // interface — real: tinygo bluetooth, mock: test double
}

// ScanResult is a discovered commissionable BLE device.
type ScanResult struct {
    Address       bluetooth.Address
    Discriminator uint16
    VendorID      uint16
    ProductID     uint16
    RSSI          int16
    Name          string
}

// Scan scans until ctx is cancelled or timeout elapses.
// Returns deduplicated results sorted by RSSI descending.
func (s *BLEScanner) Scan(ctx context.Context) ([]ScanResult, error)

// FindByDiscriminator is a convenience that scans until a device with the
// given discriminator is found or the context is cancelled.
func (s *BLEScanner) FindByDiscriminator(ctx context.Context, discriminator uint16) (*ScanResult, error)
```

### 8.3 `internal/transport/ble.go`

```go
// BLEConn implements transport.Conn over a BLE GATT connection using BTP.
// It is safe for concurrent use by multiple goroutines.
type BLEConn struct {
    device    bleDevice                       // interface wrapping tinygo Device
    c1        bleCharacteristic               // Write (commissioner → device)
    c2        bleCharacteristic               // Indicate (device → commissioner)
    btp       *btpSession
    closed    chan struct{}
    closeOnce sync.Once
}

// DialBLE connects to a Matter device at addr and completes the BTP handshake.
// Returns a ready-to-use BLEConn that satisfies transport.Conn.
func DialBLE(ctx context.Context, adapter bleAdapter, addr bluetooth.Address) (*BLEConn, error)
    // 1. adapter.Connect(ctx, addr)
    // 2. Discover Matter service UUID 0xFFF6
    // 3. Discover C1 and C2 characteristics
    // 4. Subscribe to C2 indications; route data into btpSession.handleSegment
    // 5. Send btpHandshakeRequest via C1 write
    // 6. Await HandshakeResponse on C2
    // 7. parseBTPHandshakeResponse → populate btpSession
    // 8. Return BLEConn

// Send implements transport.Conn.
// addr is ignored (BLE is point-to-point).
func (c *BLEConn) Send(ctx context.Context, msg []byte, addr net.Addr) error
    // btp.segment(msg) → write each segment to C1 (respecting flow control)

// Receive implements transport.Conn.
// Returns the next fully reassembled Matter message.
func (c *BLEConn) Receive(ctx context.Context) ([]byte, net.Addr, error)
    // block on btp.messages channel

// Close implements transport.Conn.
func (c *BLEConn) Close() error
    // close(c.closed); device.Disconnect()

// BLEAddr implements net.Addr for a BLE peer.
type BLEAddr struct{ Address bluetooth.Address }
func (a *BLEAddr) Network() string { return "ble" }
func (a *BLEAddr) String() string  { return a.Address.String() }
```

**GATT interfaces for testability:**

```go
type bleAdapter interface {
    Enable() error
    Scan(ctx context.Context, serviceUUIDs []bluetooth.UUID, cb func(ScanResult)) error
    StopScan() error
    Connect(ctx context.Context, addr bluetooth.Address) (bleDevice, error)
}

type bleDevice interface {
    DiscoverServices(uuids []bluetooth.UUID) ([]bleService, error)
    Disconnect() error
}

type bleService interface {
    DiscoverCharacteristics(uuids []bluetooth.UUID) ([]bleCharacteristic, error)
}

type bleCharacteristic interface {
    Write(data []byte) (int, error)
    EnableNotifications(cb func(data []byte)) error
}
```

The production adapter wraps `tinygo.org/x/bluetooth`. Tests use a mock adapter that pipes writes on C1 directly into the device-side btpSession and vice versa.

### 8.4 `internal/discovery/ble.go`

```go
// BLEBrowser implements commissioning.DeviceDiscoverer using BLE scanning.
type BLEBrowser struct {
    scanner *transport.BLEScanner
}

// DiscoverCommissionable satisfies commissioning.DeviceDiscoverer.
// Returns a "ble://<mac-address>" string so the controller knows to use
// BLE transport.
func (b *BLEBrowser) DiscoverCommissionable(ctx context.Context, discriminator uint16) (string, error)
```

### 8.5 `internal/controller/controller_ble.go`

```go
// ConnectPASEoverBLE establishes a PASE session with a device over BLE.
func (c *Controller) ConnectPASEoverBLE(ctx context.Context, addr bluetooth.Address, passcode uint32) (*protocol.Session, error)
    // 1. transport.DialBLE(ctx, defaultAdapter, addr) → BLEConn
    // 2. Create a temporary sub-controller with the BLEConn
    // 3. Run PASE handshake
    // 4. Return protocol.Session

// bleSessionEstablisher implements commissioning.SessionEstablisher for BLE.
// EstablishPASE connects over BLE; EstablishCASE always uses IP (UDP).
type bleSessionEstablisher struct {
    ctrl    *Controller
    adapter transport.BLEAdapter
}

func (s *bleSessionEstablisher) EstablishPASE(ctx context.Context, addr string, passcode uint32) (commissioning.Session, error)
    // Parse BLE address from "ble://<mac>" addr string
    // DialBLE → ConnectPASEoverBLE → return session

func (s *bleSessionEstablisher) EstablishCASE(ctx context.Context, addr string, nodeID uint64) (commissioning.Session, error)
    // CASE always over IP — delegate to controllerSessionEstablisher
    // (the device is on the IP network by this point)
```

### 8.6 CLI — `matter commission ble` and `--transport` flag

```
matter commission code "MT:..."
    (auto-detect: checks DiscoveryBLE bit in QR payload, prefers BLE if available)

matter commission code "MT:..." --transport ble
    (force BLE even if device also advertises on IP)

matter commission code "MT:..." --transport ip
    (force IP/mDNS even if device advertises BLE)

matter commission ble "MT:..."
    (explicit BLE subcommand — equivalent to `code --transport ble`)

matter discover ble
    (scan for BLE-commissionable devices, print table)

matter discover ble --timeout 30s
    (custom scan duration)
```

**Auto-detection logic in `newCommissionCodeCmd`:**

```
1. Parse QR code → SetupPayload
2. If transport flag == "ble":
       → use BLEBrowser + bleSessionEstablisher
   Else if transport flag == "ip":
       → use MDNSBrowser + controllerSessionEstablisher (current behaviour)
   Else (auto):
       → if payload.DiscoveryCapabilities & DiscoveryBLE != 0:
             → try BLE first (scan for 10 s)
             → if no BLE device found, fall back to mDNS
         else:
             → use mDNS
```

---

## 9. BLE Connection Lifecycle

Per Matter spec §5.5.1, the BLE connection must be closed after CommissioningComplete is confirmed. The `Commissioner.Commission()` method in `internal/commissioning/flow.go` currently does:

```
EstablishPASE → ... → EstablishCASE → CommissioningComplete
```

With BLE, the teardown is:

```
EstablishPASE (BLE) → ... network provisioning ...
    → EstablishCASE (IP — already works, no BLE needed)
    → CommissioningComplete (over CASE/IP)
    → Close BLE connection   ← new step
```

The `bleSessionEstablisher` holds a reference to the open `BLEConn`. It must close it after `EstablishCASE` succeeds (at which point BLE is no longer needed). The cleanest way to implement this:

- `bleSessionEstablisher.EstablishCASE` closes the BLE connection after IP CASE succeeds.
- If CASE fails, the BLE connection is left open for retry; it is closed on `Commissioner.Commission` returning (defer).

---

## 10. Build Tags

| Tag | Effect |
|-----|--------|
| *(no tag)* | BLE support compiled in (default) |
| `noble` | BLE support excluded — stub `DialBLE` returns `ErrBLENotSupported` |

Files:
- `internal/transport/ble.go` — `//go:build !noble`
- `internal/transport/ble_scanner.go` — `//go:build !noble`
- `internal/transport/ble_disabled.go` — `//go:build noble`
- `internal/discovery/ble.go` — `//go:build !noble`

CI can build with `go build -tags noble ./...` to verify the no-BLE path compiles cleanly.

---

## 11. Testing Strategy

| Level | What | How | Hardware? |
|-------|------|-----|-----------|
| Unit | BTP segment encode/decode | Table-driven, known byte vectors | No |
| Unit | BTP reassembly (multi-segment, boundary) | Pure byte manipulation | No |
| Unit | BTP flow control (window, seq wrap) | State machine tests | No |
| Unit | BLE advertisement parsing | Mock scan result bytes | No |
| Unit | BLEConn Send/Receive | Mock GATT adapter piping two btpSessions | No |
| Integration | BTP handshake loopback | Two btpSessions piped in-process | No |
| Integration | PASE over mock BLE | Mock BLEConn + real PASE stack | No |
| Integration | Full commissioning mock | Mock BLE + mock network + real commissioning flow | No |
| E2E | Commission real device | ESP32 Matter device or matter-js with BLE | Yes |

**Integration test tag:** `//go:build integration`

**E2E test device:** An ESP32-based Matter light or the matter-js example device (requires a machine with Bluetooth hardware). See `examples/matter-js-test-device/` for the existing test device setup.

---

## 12. Implementation Phases

### Phase 1 — BTP Protocol Engine *(no hardware required)*

**Files:** `internal/transport/btp.go`, `internal/transport/btp_test.go`

- [ ] BTP handshake request serialization (`btpHandshakeRequest`)
- [ ] BTP handshake response parsing (`parseBTPHandshakeResponse`)
- [ ] BTP segment encoding (`btpSession.segment`)
- [ ] BTP segment decoding + reassembly (`btpSession.handleSegment`)
- [ ] BTP flow control (window, sequence numbers, ack tracking)
- [ ] Standalone ack generation
- [ ] Table-driven unit tests with known byte vectors from Matter SDK

**Unblocked by:** Nothing — start immediately.

---

### Phase 2 — BLE Abstraction + Scanner *(parallel with Phase 1)*

**Files:** `internal/transport/ble_scanner.go`, `internal/transport/ble_scanner_test.go`, `internal/transport/ble_disabled.go`

- [ ] Define `bleAdapter`, `bleDevice`, `bleService`, `bleCharacteristic` interfaces
- [ ] Implement `BLEScanner` using the interface
- [ ] Implement real adapter wrapping `tinygo.org/x/bluetooth`
- [ ] Implement mock adapter for tests
- [ ] Implement `ble_disabled.go` stub (build tag `noble`)
- [ ] Add `tinygo.org/x/bluetooth` to `go.mod`

**Unblocked by:** Nothing — start immediately.

---

### Phase 3 — BLEConn

**Files:** `internal/transport/ble.go`, `internal/transport/ble_test.go`

- [ ] `DialBLE` — connect, discover service/characteristics, subscribe C2, BTP handshake
- [ ] `BLEConn.Send` — segment via BTP, write segments to C1 with flow control
- [ ] `BLEConn.Receive` — block on `btpSession.messages` channel
- [ ] `BLEConn.Close` — close BTP, disconnect GATT
- [ ] `BLEAddr` (net.Addr implementation)
- [ ] Unit tests using mock GATT adapter (pipes two btpSessions together)
- [ ] Integration test: PASE over loopback BLE mock

**Blocked by:** Phase 1 (BTP) + Phase 2 (BLE abstraction).

---

### Phase 4 — BLE Discovery

**Files:** `internal/discovery/ble.go`, `internal/discovery/ble_test.go`, update `internal/discovery/device.go`

- [ ] `BLEBrowser.Scan` — scan for `0xFFF6` advertisements, parse service data
- [ ] `BLEBrowser.DiscoverCommissionable` — returns `"ble://<mac>"` string
- [ ] Add `TransportType string` to `discovery.Device` (`"ip"` or `"ble"`)
- [ ] Unit tests with mock scan results

**Blocked by:** Phase 2 (BLE abstraction).  
**Can run in parallel with Phase 3.**

---

### Phase 5 — Controller + Commissioner Integration

**Files:** `internal/controller/controller_ble.go`, update `internal/controller/adapters.go`

- [ ] `Controller.ConnectPASEoverBLE` — DialBLE → sub-controller → PASE
- [ ] `bleSessionEstablisher` — EstablishPASE (BLE), EstablishCASE (IP), BLE close on CASE success
- [ ] Update `Controller.NewCommissioner` to accept a `transport` preference parameter
- [ ] Wire auto-detection logic: parse `DiscoveryBLE` bit, choose discoverer + establisher accordingly
- [ ] Integration test: full commissioning flow with mock BLE transport

**Blocked by:** Phase 3 (BLEConn) + Phase 4 (BLE discovery).

---

### Phase 6 — CLI Commands

**Files:** update `cli/commission.go`, update `cli/discover.go`

- [ ] Add `--transport auto|ble|ip` flag to `matter commission code`
- [ ] Auto-detection logic in `newCommissionCodeCmd` (see §8.6)
- [ ] Add `matter commission ble <setup-code>` subcommand (alias for `code --transport ble`)
- [ ] Add `matter discover ble` subcommand with `--timeout` flag
- [ ] Update `--help` and examples for all affected commands
- [ ] Test: `matter commission code` with BLE-capable QR code triggers BLE path

**Blocked by:** Phase 5 (controller integration).

---

## 13. Dependency

Add to `go.mod`:

```
tinygo.org/x/bluetooth v0.10.0
```

No other new dependencies. BTP is implemented from scratch per the Matter spec (pure Go, no library needed).

---

## 14. Platform Notes

### macOS
- Uses CoreBluetooth via `tinygo.org/x/bluetooth` (CGo, system framework — not a third-party C dep).
- Terminal app needs Bluetooth permission: System Settings → Privacy & Security → Bluetooth.
- CLI tools inherit the permission from the terminal emulator (Terminal.app, iTerm2, etc.).

### Linux
- Uses BlueZ over D-Bus via `tinygo.org/x/bluetooth` (pure Go).
- Needs `cap_net_admin` or root for BLE scanning, or the user must be in the `bluetooth` group with a permissive BlueZ policy.
- `sudo setcap 'cap_net_admin=eip' matter` or `sudo matter commission ble ...`

### Windows
- Uses WinRT via `tinygo.org/x/bluetooth` (CGo, system API).
- Requires Windows 10 1809+ for WinRT BLE Central support.

### CI / Servers (no Bluetooth)
- Build with `-tags noble` to exclude BLE.
- All BLE tests are behind `//go:build !noble` — they are skipped in this build.

---

## 15. Estimated Effort

| Phase | Effort |
|-------|--------|
| 1: BTP engine | 2–3 days |
| 2: BLE abstraction | 1–2 days |
| 3: BLEConn | 2–3 days |
| 4: BLE discovery | 1–2 days |
| 5: Controller integration | 2–3 days |
| 6: CLI commands | 1 day |
| **Total** | **~10–14 days** |

Phases 1 and 2 can run in parallel. Phase 4 can run in parallel with Phase 3.