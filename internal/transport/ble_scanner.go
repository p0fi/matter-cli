// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

// This file is part of the BLE transport layer for Matter commissioning over
// Bluetooth Low Energy. It defines the platform-agnostic GATT abstractions and
// the BLEScanner which discovers commissionable Matter devices by scanning for
// advertisements on the Matter service UUID (0xFFF6).
//
// All types in this file are independent of tinygo.org/x/bluetooth so that
// tests can use a pure-Go mock adapter without CGo or hardware.
//
// Build tag:
//   - default (no tag): BLE support compiled in.
//   - noble: BLE support excluded; see ble_disabled.go for stubs.
package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

// ─── BLE service / characteristic UUID constants ─────────────────────────────

// Matter BLE service and characteristic UUIDs as defined in Matter spec §5.5.

const (
	// MatterServiceUUID is the 128-bit UUID for the Matter BLE service.
	MatterServiceUUID BLEUUID = "0000fff6-0000-1000-8000-00805f9b34fb"

	// MatterC1UUID is the commissioner-to-device write characteristic UUID.
	MatterC1UUID BLEUUID = "18ee2ef5-263d-4559-959f-4f9c429f9d11"

	// MatterC2UUID is the device-to-commissioner indicate characteristic UUID.
	MatterC2UUID BLEUUID = "18ee2ef5-263d-4559-959f-4f9c429f9d12"

	// MatterC3UUID is the optional additional-data read characteristic UUID.
	MatterC3UUID BLEUUID = "64630238-8772-45f2-b87d-748a83218f04"
)

// ─── Opaque platform address and UUID types ───────────────────────────────────

// BLEAddress is a platform-specific opaque device identifier.
//
// On Linux it is a "AA:BB:CC:DD:EE:FF" string (Bluetooth MAC address).
// On macOS it is a CoreBluetooth-assigned UUID string of the form
// "12345678-1234-1234-1234-123456789ABC" because CoreBluetooth does not expose
// hardware MAC addresses.
//
// Addresses obtained from BLEScanner.Scan or BLEScanner.FindByDiscriminator
// should be treated as opaque tokens and passed directly to bleAdapter.Connect
// or DialBLE without modification.
type BLEAddress string

// String returns the string representation of the address.
func (a BLEAddress) String() string { return string(a) }

// Network returns the network name for use with net.Addr.
func (a BLEAddress) Network() string { return "ble" }

// BLEUUID is a 128-bit Bluetooth UUID represented in lowercase canonical form
// "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx".
type BLEUUID string

// String returns the canonical lowercase UUID string.
func (u BLEUUID) String() string { return string(u) }

// ─── Advertisement payload ────────────────────────────────────────────────────

// BLEScanAdvertisement is the raw event delivered by the adapter layer for
// each observed BLE advertisement. It is an adapter-agnostic type; the real
// tinygo adapter and the mock adapter both produce values of this type.
type BLEScanAdvertisement struct {
	// Address is the advertising device's opaque platform identifier.
	Address BLEAddress

	// RSSI is the received signal strength in dBm.
	RSSI int16

	// LocalName is the Bluetooth device name from the advertisement, if present.
	LocalName string

	// ServiceData maps service UUID (canonical lowercase string) to the raw
	// service data bytes carried in the advertisement.
	ServiceData map[BLEUUID][]byte
}

// ─── Scan result ──────────────────────────────────────────────────────────────

// ScanResult is a fully parsed commissionable Matter device discovered by BLE scan.
// Fields are populated by parsing the Matter-specific service data on UUID 0xFFF6.
type ScanResult struct {
	// Address is the opaque platform address, suitable for passing to DialBLE.
	Address BLEAddress

	// Discriminator is the 12-bit commissioning discriminator from the
	// advertisement payload (Matter spec §5.4.2.5.6, bits [11:0]).
	Discriminator uint16

	// VendorID is the 16-bit vendor identifier from the advertisement.
	VendorID uint16

	// ProductID is the 16-bit product identifier from the advertisement.
	ProductID uint16

	// RSSI is the signal strength at the time the advertisement was seen.
	RSSI int16

	// Name is the Bluetooth local name from the advertisement, if present.
	Name string
}

// ─── GATT abstraction interfaces ─────────────────────────────────────────────
//
// These interfaces isolate the BLE stack (tinygo.org/x/bluetooth on real
// hardware, a mock double in tests) from the rest of the transport layer.
// All methods use our own BLEAddress / BLEUUID types so callers need not
// import tinygo.

// BLEAdapter is the platform BLE adapter abstraction used by BLEScanner and
// BLEConn. The real implementation wraps tinygo.org/x/bluetooth; tests use
// a mock.
type BLEAdapter interface {
	// Enable initialises the BLE adapter. Must be called before Scan or Connect.
	Enable() error

	// Scan starts a BLE scan and invokes cb for each observed advertisement.
	// It runs until the context is cancelled, after which it returns ctx.Err().
	// If StopScan is called explicitly, Scan returns nil.
	// Only one concurrent scan is supported; a second call returns an error.
	Scan(ctx context.Context, cb func(BLEScanAdvertisement)) error

	// StopScan stops an in-progress scan. Safe to call from within cb.
	StopScan() error

	// Connect establishes a GATT connection to the device at addr.
	// Returns a BLEDevice ready for service/characteristic discovery.
	Connect(ctx context.Context, addr BLEAddress) (BLEDevice, error)
}

// Internal aliases for backward compatibility with existing code.
type bleAdapter = BLEAdapter
type bleDevice = BLEDevice
type bleService = BLEService
type bleCharacteristic = BLECharacteristic

// BLEDevice represents an open GATT connection to a remote BLE device.
type BLEDevice interface {
	// DiscoverServices discovers GATT services. Pass nil to discover all; pass
	// a list of UUIDs to filter to specific services.
	DiscoverServices(uuids []BLEUUID) ([]BLEService, error)

	// Disconnect closes the GATT connection.
	Disconnect() error
}

// BLEService represents a single GATT service on a connected device.
type BLEService interface {
	// UUID returns the 128-bit UUID identifying this service.
	UUID() BLEUUID

	// DiscoverCharacteristics discovers characteristics within the service.
	// Pass nil to discover all; pass a list of UUIDs to filter.
	DiscoverCharacteristics(uuids []BLEUUID) ([]BLECharacteristic, error)
}

// BLECharacteristic represents a single GATT characteristic.
type BLECharacteristic interface {
	// UUID returns the 128-bit UUID identifying this characteristic.
	UUID() BLEUUID

	// Write performs a GATT Write Without Response to the characteristic.
	// Returns the number of bytes written and any error.
	// On macOS, returns (-2, nil) when canSendWriteWithoutResponse is false
	// (the write would be silently dropped); the caller must wait and retry.
	Write(data []byte) (int, error)

	// WriteWithResponse performs a GATT Write Request (with ATT-level response)
	// to the characteristic. Used as a last-resort fallback when Write Without
	// Response consistently fails to elicit a reply from the device. Some
	// peripherals support both Write and Write Without Response on C1 — the
	// ATT Write Request may succeed even when the ATT Write Command is dropped.
	WriteWithResponse(data []byte) (int, error)

	// EnableNotifications registers cb to be called whenever the remote device
	// sends an indication or notification on this characteristic.
	// Calling EnableNotifications a second time replaces the previous callback.
	//
	// On macOS this also triggers an asynchronous CCCD write via CoreBluetooth
	// (tinygo calls setNotifyValue:YES internally). The write is asynchronous —
	// call WaitForNotifying afterward to confirm the subscription is active
	// before writing any request that expects an indication response.
	EnableNotifications(cb func(data []byte)) error

	// WaitForNotifying blocks until the characteristic's CCCD subscription is
	// fully acknowledged by the peripheral (i.e. isNotifying becomes true).
	// On macOS this polls CBCharacteristic.isNotifying via CoreBluetooth; on
	// other platforms (or when the raw pointer is unavailable) it returns
	// immediately since EnableNotifications is synchronous there.
	//
	// This MUST be called after EnableNotifications and before writing any
	// request that expects an indication response.
	WaitForNotifying(ctx context.Context) error

	// ClearCachedValue discards any previously cached indication/notification
	// value so that a subsequent WaitForValue call will block until genuinely
	// new data arrives. On macOS the clear is dispatched to bt_queue for
	// thread safety. Call this just before writing a request that expects an
	// indication response to avoid stale data being returned.
	ClearCachedValue()

	// WaitForValue polls the characteristic's cached value until it becomes
	// non-nil or the context is cancelled. This provides a reliable fallback
	// for reading indication/notification data when the BLE stack's callback
	// delivery is unreliable (e.g. tinygo bluetooth on macOS).
	//
	// The caller is responsible for calling ClearCachedValue before issuing
	// any request whose response will be delivered as an indication — WaitForValue
	// no longer clears the value internally.
	//
	// The returned slice is a snapshot of the value at the time it was
	// observed. Callers should treat it as read-only.
	WaitForValue(ctx context.Context) ([]byte, error)

	// ReadAndClearCachedValue atomically reads and clears the characteristic's
	// cached indication/notification value in a single bt_queue-dispatched
	// operation. Returns nil if no value is currently cached. Use this in a
	// polling loop for ongoing data delivery — the atomic read+clear prevents
	// the race where a second indication arrives between a separate read and
	// a separate clear.
	ReadAndClearCachedValue() []byte

	// IsConnected returns whether the peripheral that owns this characteristic
	// is still in the connected state. On macOS this checks
	// CBPeripheral.state == CBPeripheralStateConnected via CoreBluetooth.
	// On other platforms (or when the raw pointer is unavailable) it returns
	// true optimistically.
	//
	// Used by the C2 data-polling goroutine to detect peripheral disconnection
	// and tear down the BLE connection promptly instead of hanging forever
	// waiting for data that will never arrive.
	IsConnected() bool
}

// ─── BLEScanner ──────────────────────────────────────────────────────────────

// BLEScanner scans for Matter devices advertising on the Matter BLE service
// UUID (0xFFF6) and parses the Matter-specific advertisement service data into
// ScanResult values.
//
// Use NewBLEScanner to create a scanner. Provide a bleAdapter implementation:
// on real hardware use NewDefaultBLEAdapter(); in tests use a mockBLEAdapter.
type BLEScanner struct {
	adapter bleAdapter
}

// NewBLEScanner creates a new BLEScanner backed by the given adapter.
// The adapter must not be nil.
func NewBLEScanner(adapter bleAdapter) *BLEScanner {
	if adapter == nil {
		panic("transport: NewBLEScanner called with nil adapter")
	}
	return &BLEScanner{adapter: adapter}
}

// Scan scans for commissionable Matter devices until ctx is cancelled.
//
// It returns a deduplicated list of discovered devices sorted by RSSI
// descending (strongest signal first). The function blocks until ctx is
// done and returns a non-nil error only if the underlying adapter scan fails.
//
// If ctx is cancelled cleanly, Scan returns (results, nil) — the context
// cancellation is the normal termination mechanism.
//
// Example (scan for 10 seconds):
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	results, err := scanner.Scan(ctx)
func (s *BLEScanner) Scan(ctx context.Context) ([]ScanResult, error) {
	var mu sync.Mutex
	// seen deduplicates by address; we keep the highest-RSSI observation.
	seen := make(map[BLEAddress]*ScanResult)

	scanErr := s.adapter.Scan(ctx, func(adv BLEScanAdvertisement) {
		sr, ok := parseMatterAdvertisement(adv)
		if !ok {
			return // not a Matter advertisement
		}

		mu.Lock()
		defer mu.Unlock()
		if existing, dup := seen[adv.Address]; dup {
			// Update RSSI if this sighting is stronger.
			if sr.RSSI > existing.RSSI {
				existing.RSSI = sr.RSSI
			}
			return
		}
		seen[adv.Address] = &sr
	})

	// A cancelled context is the normal way to end a scan — not an error.
	if scanErr == context.Canceled || scanErr == context.DeadlineExceeded {
		scanErr = nil
	}

	mu.Lock()
	results := make([]ScanResult, 0, len(seen))
	for _, r := range seen {
		results = append(results, *r)
	}
	mu.Unlock()

	sort.Slice(results, func(i, j int) bool {
		return results[i].RSSI > results[j].RSSI
	})

	return results, scanErr
}

// FindByDiscriminator scans until a device with the given discriminator is
// found or the context is cancelled.
//
// When the lower 8 bits of discriminator are zero the caller only has the
// 4-bit short discriminator (from a manual pairing code). In that case the
// match is performed on the upper 4 bits only, mirroring the mDNS discoverer
// behaviour.
//
// Returns a pointer to the matching ScanResult, or an error if no device was
// found before the context expired.
func (s *BLEScanner) FindByDiscriminator(ctx context.Context, discriminator uint16) (*ScanResult, error) {
	// Detect short-discriminator mode: manual pairing codes encode only
	// the upper 4 bits and store them as shortDisc<<8, leaving bits [7:0]
	// zero. When we see this pattern we match on the short discriminator.
	shortMatch := discriminator&0xFF == 0
	shortDisc := discriminator >> 8

	found := make(chan ScanResult, 1)
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	scanErr := s.adapter.Scan(scanCtx, func(adv BLEScanAdvertisement) {
		sr, ok := parseMatterAdvertisement(adv)
		if !ok {
			return
		}
		var matches bool
		if shortMatch {
			matches = sr.Discriminator>>8 == shortDisc
		} else {
			matches = sr.Discriminator == discriminator
		}
		if !matches {
			return
		}
		// Non-blocking send: if the channel already has a result, ignore.
		select {
		case found <- sr:
			cancel() // stop the scan
		default:
		}
	})

	// A context cancellation is the normal stop mechanism.
	if scanErr == context.Canceled || scanErr == context.DeadlineExceeded {
		scanErr = nil
	}
	if scanErr != nil {
		return nil, fmt.Errorf("BLE scan: %w", scanErr)
	}

	select {
	case sr := <-found:
		return &sr, nil
	default:
		// Scan ended without finding the discriminator.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("BLE scan timed out: no device with discriminator %d found", discriminator)
		}
		return nil, fmt.Errorf("BLE scan ended: no device with discriminator %d found", discriminator)
	}
}

// ─── Matter advertisement parser ─────────────────────────────────────────────

// matterAdvOpCode is the OpCode byte expected in a Matter commissionable
// advertisement service data payload (Matter spec §5.4.2.5.6, byte 0 = 0x00).
const matterAdvOpCode = uint8(0x00)

// matterAdvMinLen is the minimum length of a valid Matter service data payload.
// Byte layout: [opcode:1][disc+ver:2][VID:2][PID:2][flags:1] = 8 bytes minimum
// (The spec shows 8 bytes; byte 7 is the Additional Data flag byte.)
const matterAdvMinLen = 8

// parseMatterAdvertisement attempts to parse a Matter commissioning
// advertisement from adv.  It returns the ScanResult and ok=true if the
// advertisement contains a valid Matter service data payload on UUID 0xFFF6,
// or the zero value and ok=false otherwise.
//
// Wire layout per Matter spec §5.4.2.5.6:
//
//	Byte 0:     OpCode     (must be 0x00)
//	Byte 1–2:  Discriminator [11:0] + version nibble [15:12] (uint16 LE)
//	Byte 3–4:  Vendor ID  (uint16 LE)
//	Byte 5–6:  Product ID (uint16 LE)
//	Byte 7:    Additional Data flag (bit 0 = C3 data present)
//
// On some platforms (notably macOS with CoreBluetooth) the service data
// payload may not be delivered even though the Matter service UUID (0xFFF6) is
// present in the "List of Service UUIDs" AD type.  In that case the adapter
// layer synthesises a nil entry in ServiceData so that this function can still
// surface the device.  A ScanResult with zero Discriminator/VID/PID is
// returned so callers know the device exists but the detailed payload was not
// available.
func parseMatterAdvertisement(adv BLEScanAdvertisement) (ScanResult, bool) {
	data, ok := adv.ServiceData[MatterServiceUUID]
	if !ok {
		return ScanResult{}, false
	}

	// Partial advertisement: the Matter service UUID was observed in the
	// ServiceUUIDs list but CoreBluetooth did not include service data.
	// Return a minimal ScanResult so the device is at least visible.
	if len(data) == 0 {
		return ScanResult{
			Address: adv.Address,
			RSSI:    adv.RSSI,
			Name:    adv.LocalName,
		}, true
	}

	if len(data) < matterAdvMinLen {
		return ScanResult{}, false
	}
	if data[0] != matterAdvOpCode {
		return ScanResult{}, false
	}

	discAndVer := binary.LittleEndian.Uint16(data[1:3])
	discriminator := discAndVer & 0x0FFF // bits [11:0]

	vendorID := binary.LittleEndian.Uint16(data[3:5])
	productID := binary.LittleEndian.Uint16(data[5:7])

	return ScanResult{
		Address:       adv.Address,
		Discriminator: discriminator,
		VendorID:      vendorID,
		ProductID:     productID,
		RSSI:          adv.RSSI,
		Name:          adv.LocalName,
	}, true
}
