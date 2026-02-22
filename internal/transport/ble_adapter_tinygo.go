// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

// This file provides the production bleAdapter implementation that wraps
// tinygo.org/x/bluetooth. It is compiled on all platforms where BLE support
// is enabled (i.e., when the "noble" build tag is NOT set).
//
// Platform notes:
//   - macOS: uses CoreBluetooth via CGo (tinygo-org/cbgo). The process needs
//     Bluetooth permission granted to the terminal app.
//   - Linux: uses BlueZ over D-Bus (no CGo). The user needs cap_net_admin or
//     membership in the "bluetooth" group.
//   - Windows: uses WinRT via CGo (requires Windows 10 1809+).
//
// Tests should use the mockBLEAdapter defined in ble_scanner_test.go instead
// of this adapter to avoid CGo/hardware requirements.
package transport

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"
	"unsafe"

	ble "tinygo.org/x/bluetooth"
)

// ─── tinygoAdapter ────────────────────────────────────────────────────────────

// tinygoAdapter wraps a *bluetooth.Adapter to implement the bleAdapter
// interface. It translates between the platform-agnostic BLEAddress / BLEUUID
// types used throughout this package and the concrete types exposed by the
// tinygo bluetooth library.
type tinygoAdapter struct {
	a          *ble.Adapter
	enableOnce sync.Once
	enableErr  error
}

// NewDefaultBLEAdapter returns a BLEAdapter backed by the system's default
// Bluetooth adapter (bluetooth.DefaultAdapter). Call Enable() on the returned
// adapter before using it.
func NewDefaultBLEAdapter() BLEAdapter {
	return &tinygoAdapter{a: ble.DefaultAdapter}
}

// Enable initialises the underlying Bluetooth adapter hardware.
// It is safe to call multiple times; the actual initialisation runs only once.
func (t *tinygoAdapter) Enable() error {
	t.enableOnce.Do(func() {
		t.enableErr = t.a.Enable()
	})
	return t.enableErr
}

// Scan starts a BLE scan and calls cb for each observed advertisement that
// contains a service data entry. Scanning runs until ctx is cancelled or
// StopScan is called.
//
// The tinygo bluetooth Scan call is blocking: it runs until StopScan() is
// called. We launch it in a goroutine and call StopScan when ctx is done.
func (t *tinygoAdapter) Scan(ctx context.Context, cb func(BLEScanAdvertisement)) error {
	errCh := make(chan error, 1)

	slog.Debug("ble: scan started")

	go func() {
		errCh <- t.a.Scan(func(adapter *ble.Adapter, r ble.ScanResult) {
			// Build our platform-agnostic advertisement.
			adv := BLEScanAdvertisement{
				Address:     bleAddressFromTinygo(r.Address),
				RSSI:        r.RSSI,
				LocalName:   r.LocalName(),
				ServiceData: make(map[BLEUUID][]byte),
			}
			for _, sd := range r.ServiceData() {
				adv.ServiceData[BLEUUID(sd.UUID.String())] = append([]byte(nil), sd.Data...)
			}

			// If the Matter service UUID appears in the ServiceUUIDs list but
			// CoreBluetooth did not populate ServiceData (which happens on some
			// macOS versions / devices), synthesise an empty entry so that
			// downstream code at least knows this is a Matter device. The
			// BLEScanner will skip devices with too-short service data, but
			// future code can handle the partial case.
			matterUUID := ble.New16BitUUID(0xFFF6)
			if _, hasSvcData := adv.ServiceData[MatterServiceUUID]; !hasSvcData {
				if r.HasServiceUUID(matterUUID) {
					slog.Debug("ble: Matter UUID present in ServiceUUIDs but no ServiceData — device in commissioning mode without payload")
					adv.ServiceData[MatterServiceUUID] = nil
				}
			}

			// Emit a debug log line for every advertisement so operators can
			// diagnose scan issues with -v / --verbose.
			if slog.Default().Enabled(ctx, slog.LevelDebug) {
				svcDataKeys := make([]string, 0, len(adv.ServiceData))
				svcDataHex := make([]string, 0, len(adv.ServiceData))
				for k, v := range adv.ServiceData {
					svcDataKeys = append(svcDataKeys, string(k))
					svcDataHex = append(svcDataHex, hex.EncodeToString(v))
				}
				// Build a human-readable service-UUID list by probing common
				// short UUIDs. tinygo's AdvertisementPayload doesn't expose a
				// ServiceUUIDs() accessor directly on ScanResult, but we can
				// check the service data keys and the HasServiceUUID helper.
				var svcFlags []string
				if r.HasServiceUUID(matterUUID) {
					svcFlags = append(svcFlags, "Matter(0xFFF6)")
				}
				slog.Debug("ble: advertisement",
					"addr", adv.Address,
					"rssi", adv.RSSI,
					"name", adv.LocalName,
					"svcFlags", strings.Join(svcFlags, ","),
					"svcDataKeys", svcDataKeys,
					"svcDataHex", svcDataHex,
				)
			}

			cb(adv)
		})
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Context expired — stop the scan and wait for the goroutine to finish.
		_ = t.a.StopScan()
		<-errCh
		return ctx.Err()
	}
}

// StopScan stops an in-progress scan.
func (t *tinygoAdapter) StopScan() error {
	return t.a.StopScan()
}

// Connect establishes a GATT connection to the device identified by addr.
// The connection attempt is bounded by ctx; if ctx is cancelled before the
// connection succeeds, Connect returns ctx.Err().
//
// Note: the underlying tinygo Connect call uses its own internal timeout
// (10 s on macOS). Context cancellation is best-effort — it interrupts the
// wait but cannot abort a connection that CoreBluetooth has already initiated.
func (t *tinygoAdapter) Connect(ctx context.Context, addr BLEAddress) (bleDevice, error) {
	bleAddr, err := bleAddressToTinygo(addr)
	if err != nil {
		return nil, fmt.Errorf("ble: invalid address %q: %w", addr, err)
	}

	type result struct {
		dev ble.Device
		err error
	}
	ch := make(chan result, 1)

	go func() {
		d, err := t.a.Connect(bleAddr, ble.ConnectionParams{})
		ch <- result{dev: d, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("ble: connect to %s: %w", addr, r.err)
		}
		return &tinygoDevice{d: r.dev}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("ble: connect to %s: %w", addr, ctx.Err())
	}
}

// ─── tinygoDevice ─────────────────────────────────────────────────────────────

// tinygoDevice wraps a bluetooth.Device to implement bleDevice.
type tinygoDevice struct {
	d ble.Device
}

// DiscoverServices discovers GATT services on the connected device.
// Pass nil to discover all services; pass a list to filter by UUID.
func (td *tinygoDevice) DiscoverServices(uuids []BLEUUID) ([]bleService, error) {
	bleUUIDs, err := convertUUIDs(uuids)
	if err != nil {
		return nil, err
	}
	svcs, err := td.d.DiscoverServices(bleUUIDs)
	if err != nil {
		return nil, fmt.Errorf("ble: discover services: %w", err)
	}
	result := make([]bleService, len(svcs))
	for i, s := range svcs {
		result[i] = &tinygoService{s: s}
	}
	return result, nil
}

// Disconnect closes the GATT connection to the device.
func (td *tinygoDevice) Disconnect() error {
	return td.d.Disconnect()
}

// ─── tinygoService ────────────────────────────────────────────────────────────

// tinygoService wraps a bluetooth.DeviceService to implement bleService.
type tinygoService struct {
	s ble.DeviceService
}

// UUID returns the 128-bit UUID of this GATT service.
func (ts *tinygoService) UUID() BLEUUID {
	return BLEUUID(ts.s.UUID().String())
}

// DiscoverCharacteristics discovers characteristics within this service.
// Pass nil to discover all; pass a list to filter by UUID.
func (ts *tinygoService) DiscoverCharacteristics(uuids []BLEUUID) ([]bleCharacteristic, error) {
	bleUUIDs, err := convertUUIDs(uuids)
	if err != nil {
		return nil, err
	}
	chars, err := ts.s.DiscoverCharacteristics(bleUUIDs)
	if err != nil {
		return nil, fmt.Errorf("ble: discover characteristics: %w", err)
	}
	result := make([]bleCharacteristic, len(chars))
	for i, c := range chars {
		result[i] = &tinygoCharacteristic{c: c, rawPtr: extractCBCharacteristicPtr(c)}
	}
	return result, nil
}

// ─── tinygoCharacteristic ─────────────────────────────────────────────────────

// tinygoCharacteristic wraps a bluetooth.DeviceCharacteristic to implement
// bleCharacteristic.
type tinygoCharacteristic struct {
	c ble.DeviceCharacteristic

	// rawPtr holds the raw unsafe.Pointer to the underlying platform
	// characteristic object (e.g. CBCharacteristic* on macOS). It is
	// extracted at construction time via extractCBCharacteristicPtr and
	// used by WaitForValue to poll the cached value directly through
	// CoreBluetooth, bypassing the tinygo notification callback mechanism
	// which has known reliability issues on macOS.
	rawPtr unsafe.Pointer
}

// UUID returns the 128-bit UUID of this GATT characteristic.
func (tc *tinygoCharacteristic) UUID() BLEUUID {
	return BLEUUID(tc.c.UUID().String())
}

// Write performs a GATT Write Without Response on the characteristic.
// Matter BTP (§4.15) specifies C1 as write-without-response for both the
// BTP Capabilities Request and all subsequent data segments.
//
// On macOS, the write is dispatched to bt_queue using the fresh (live)
// CBCharacteristic pointer. This is the same stale-pointer fix applied to
// setNotifyValue:YES — CoreBluetooth silently rejects writeValue:forCharacteristic:
// with CBError code 8 when the pointer passed is not pointer-identical to the
// live object in svc.characteristics. Calling from a goroutine thread instead
// of bt_queue also risks silent failure on some macOS versions.
//
// Falls back to tinygo's WriteWithoutResponse if rawPtr is nil (non-macOS,
// extraction failed) or if bt_queue is not yet initialised.
func (tc *tinygoCharacteristic) Write(data []byte) (int, error) {
	if tc.rawPtr != nil {
		n := corebtWriteWithoutResponse(tc.rawPtr, data)
		if n >= 0 {
			slog.Debug("ble: C1 write dispatched to bt_queue", "bytes", n)
			return n, nil
		}
		// bt_queue not ready or nil pointer chain — fall through to tinygo path.
		slog.Debug("ble: C1 write via bt_queue failed (nil chain or bt_queue), falling back to tinygo write")
	}
	return tc.c.WriteWithoutResponse(data)
}

// EnableNotifications registers cb to be called on each indication or
// notification received from the device on this characteristic.
//
// On macOS, tinygo internally calls [peripheral setNotifyValue:YES …] which
// is asynchronous with respect to CoreBluetooth's bt_queue. Always call
// WaitForNotifying afterward to confirm the subscription before proceeding.
func (tc *tinygoCharacteristic) EnableNotifications(cb func(data []byte)) error {
	return tc.c.EnableNotifications(cb)
}

// WaitForNotifying blocks until the characteristic's CCCD subscription is
// fully acknowledged by the peripheral (isNotifying becomes true). On macOS
// this polls CBCharacteristic.isNotifying via CoreBluetooth; on other
// platforms (or when the raw pointer could not be extracted) it returns
// immediately since EnableNotifications is synchronous on those stacks.
//
// If tinygo's EnableNotifications didn't actually write the CCCD (which can
// happen on macOS without a developer Bluetooth profile), this method falls
// back to issuing setNotifyValue:YES directly on bt_queue and waits for
// isNotifying to become true.
func (tc *tinygoCharacteristic) WaitForNotifying(ctx context.Context) error {
	if tc.rawPtr == nil {
		// No raw pointer available (non-macOS or extraction failed).
		// EnableNotifications is synchronous on these platforms.
		return nil
	}

	// Fast path: tinygo's async CCCD write already landed.
	if corebtIsNotifying(tc.rawPtr) {
		return nil
	}

	// Give tinygo's async CCCD write time to complete. On macOS with a
	// developer Bluetooth profile the write typically lands within ~100 ms.
	// 500 ms is generous for a single ATT round-trip over BLE.
	briefCtx, briefCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer briefCancel()
	if err := corebtWaitNotifying(briefCtx, tc.rawPtr); err == nil {
		slog.Debug("ble: WaitForNotifying: tinygo subscription confirmed (isNotifying=true)")
		return nil
	}

	// tinygo's call failed silently. This happens on macOS without a
	// developer Bluetooth profile: tinygo calls setNotifyValue from a Go
	// goroutine thread instead of on cbgo's bt_queue, so CoreBluetooth
	// accepts the call but never sends the CCCD write to the peripheral.
	//
	// Reset any partial state by unsubscribing first (setNotifyValue:NO),
	// then re-subscribe cleanly on bt_queue.
	slog.Debug("ble: WaitForNotifying: tinygo CCCD write timed out — falling back to direct CoreBluetooth repair")

	if corebtUnsubscribe(tc.rawPtr) {
		slog.Debug("ble: WaitForNotifying: sent setNotifyValue:NO to reset partial subscription state")
		// Give CoreBluetooth a moment to process the unsubscribe before
		// we re-subscribe. 150 ms covers the bt_queue round-trip.
		time.Sleep(150 * time.Millisecond)
	}

	if !corebtSubscribe(tc.rawPtr) {
		slog.Debug("ble: WaitForNotifying: direct subscribe failed (nil peripheral chain or bt_queue)")
		// Proceed anyway — let the notification callback or value poll race.
		return nil
	}
	slog.Debug("ble: WaitForNotifying: sent setNotifyValue:YES on bt_queue")

	if err := corebtWaitNotifying(ctx, tc.rawPtr); err != nil {
		slog.Debug("ble: WaitForNotifying: isNotifying still false after repair",
			"err", err,
			"isNotifying", corebtIsNotifying(tc.rawPtr))
		// Don't fail — proceed and let the poll/callback paths race.
		return nil
	}
	slog.Debug("ble: WaitForNotifying: subscription confirmed after repair (isNotifying=true)")
	return nil
}

// WaitForValue polls the characteristic's cached value via CoreBluetooth
// until it becomes non-nil or the context is cancelled. This bypasses the
// tinygo bluetooth library's notification callback dispatch, which has known
// reliability issues with indicate characteristics on macOS (the pointer-
// based characteristic matching in DidUpdateValueForCharacteristic can fail
// silently).
//
// On non-macOS platforms (or if the raw pointer was not captured), this
// falls back to blocking on the context — the caller should always also
// register a notification callback via EnableNotifications as a parallel
// path.
func (tc *tinygoCharacteristic) WaitForValue(ctx context.Context) ([]byte, error) {
	if tc.rawPtr == nil {
		// No raw pointer available (non-macOS or extraction failed).
		// Block until context expires; the caller's notification callback
		// is the only delivery path.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	// Resolve the fresh CoreBluetooth pointer. The cbgo-cached pointer may
	// be stale (a different object than what CoreBluetooth tracks), which
	// means polling its .value property would never see indication data.
	freshPtr, wasStale := corebtGetFreshPtr(tc.rawPtr)
	if wasStale {
		slog.Debug("ble: WaitForValue: resolved stale cbgo pointer to fresh CoreBluetooth pointer",
			"stalePtr", fmt.Sprintf("%p", tc.rawPtr),
			"freshPtr", fmt.Sprintf("%p", freshPtr),
		)
		tc.rawPtr = freshPtr
	}

	// NOTE: The caller is responsible for calling ClearCachedValue() BEFORE
	// writing any request that will trigger an indication response. We no
	// longer clear here to avoid the race where the device responds faster
	// than this goroutine starts and we'd erase the already-delivered value.
	return corebtPollValue(ctx, tc.rawPtr)
}

// ClearCachedValue discards any previously cached indication value so that a
// subsequent WaitForValue call only returns genuinely new data. The clear is
// dispatched to bt_queue for thread safety. Must be called BEFORE writing a
// request that expects an indication response.
func (tc *tinygoCharacteristic) ClearCachedValue() {
	if tc.rawPtr == nil {
		return
	}
	// Resolve the fresh pointer before clearing; the stale pointer's value
	// field may not be the one CoreBluetooth writes to.
	freshPtr, wasStale := corebtGetFreshPtr(tc.rawPtr)
	if wasStale {
		slog.Debug("ble: ClearCachedValue: resolved stale pointer to fresh",
			"stalePtr", fmt.Sprintf("%p", tc.rawPtr),
			"freshPtr", fmt.Sprintf("%p", freshPtr),
		)
		tc.rawPtr = freshPtr
	}
	corebtClearValue(tc.rawPtr)
}

// ReadAndClearCachedValue atomically reads and clears the characteristic's
// cached indication value in a single bt_queue-dispatched operation. Returns
// nil if no value is currently available. Used by the C2 data-polling loop
// for reliable ongoing BTP segment delivery.
func (tc *tinygoCharacteristic) ReadAndClearCachedValue() []byte {
	if tc.rawPtr == nil {
		return nil
	}
	return corebtReadAndClear(tc.rawPtr)
}

// extractCBCharacteristicPtr uses reflection + unsafe to extract the raw
// platform characteristic pointer (CBCharacteristic* on macOS, or equivalent)
// from a tinygo bluetooth.DeviceCharacteristic.
//
// DeviceCharacteristic layout (tinygo.org/x/bluetooth v0.10–v0.14, darwin):
//
//	type DeviceCharacteristic struct { *deviceCharacteristic }
//	type deviceCharacteristic struct {
//	    uuidWrapper                          // UUID = [4]uint32, 16 bytes
//	    service        DeviceService         // struct { *deviceService }, 8 bytes
//	    characteristic cbgo.Characteristic   // struct { ptr unsafe.Pointer }, 8 bytes
//	    ...
//	}
//
// Rather than assuming a fixed field index (which breaks across tinygo
// bluetooth versions), we search all fields of the inner struct for one
// that looks like cbgo.Characteristic: a struct with exactly one field of
// kind UnsafePointer. This is robust against field reordering or the
// addition of new fields in future versions.
//
// If no matching field is found, returns nil — callers fall back to the
// tinygo notification callback as the sole delivery path.
func extractCBCharacteristicPtr(dc ble.DeviceCharacteristic) unsafe.Pointer {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("ble: failed to extract raw characteristic pointer (struct layout may have changed)", "err", r)
		}
	}()

	// dc is DeviceCharacteristic{ *deviceCharacteristic }.
	// Field(0) is the embedded *deviceCharacteristic pointer.
	dcVal := reflect.ValueOf(dc)
	if dcVal.Kind() != reflect.Struct || dcVal.NumField() < 1 {
		slog.Debug("ble: extractCBCharacteristicPtr: outer type is not a struct or has no fields",
			"kind", dcVal.Kind(), "numField", dcVal.NumField())
		return nil
	}
	ptrField := dcVal.Field(0) // *deviceCharacteristic
	if ptrField.Kind() != reflect.Pointer || ptrField.IsNil() {
		slog.Debug("ble: extractCBCharacteristicPtr: field 0 is not a non-nil pointer",
			"kind", ptrField.Kind())
		return nil
	}
	inner := ptrField.Elem() // deviceCharacteristic struct
	if inner.Kind() != reflect.Struct {
		slog.Debug("ble: extractCBCharacteristicPtr: inner type is not a struct",
			"kind", inner.Kind())
		return nil
	}

	innerType := inner.Type()
	numFields := inner.NumField()

	// Log the full struct layout for diagnostics (only on first call).
	if slog.Default().Enabled(nil, slog.LevelDebug) {
		fields := make([]string, numFields)
		for i := 0; i < numFields; i++ {
			f := innerType.Field(i)
			fields[i] = fmt.Sprintf("%d:%s(%s/%s)", i, f.Name, f.Type, inner.Field(i).Kind())
		}
		slog.Debug("ble: extractCBCharacteristicPtr: inner struct layout",
			"type", innerType.Name(), "fields", fields)
	}

	// Search for a field that looks like cbgo.Characteristic: a struct
	// containing exactly one field of kind UnsafePointer. We check the
	// field name contains "haracteristic" (case-insensitive match on
	// "characteristic" or "Characteristic") to avoid false positives on
	// other single-pointer structs like cbgo.Service.
	for i := 0; i < numFields; i++ {
		f := innerType.Field(i)
		fv := inner.Field(i)

		if fv.Kind() != reflect.Struct || fv.NumField() != 1 {
			continue
		}
		// Check the inner field is an unsafe.Pointer.
		if fv.Field(0).Kind() != reflect.UnsafePointer {
			continue
		}
		// Prefer the field named "characteristic" but accept any match
		// if there's only one candidate.
		if !strings.Contains(strings.ToLower(f.Name), "haracteristic") {
			continue
		}
		if !fv.Field(0).CanAddr() {
			slog.Debug("ble: extractCBCharacteristicPtr: found characteristic field but cannot addr",
				"fieldIndex", i, "fieldName", f.Name)
			return nil
		}
		rawPtr := *(*unsafe.Pointer)(unsafe.Pointer(fv.Field(0).UnsafeAddr()))
		slog.Debug("ble: extractCBCharacteristicPtr: extracted pointer",
			"fieldIndex", i, "fieldName", f.Name, "ptr", fmt.Sprintf("%p", rawPtr))
		return rawPtr
	}

	// Fallback: if no field matched by name, try any single-UnsafePointer
	// struct field. This handles unexpected renames.
	for i := 0; i < numFields; i++ {
		fv := inner.Field(i)
		if fv.Kind() != reflect.Struct || fv.NumField() != 1 {
			continue
		}
		if fv.Field(0).Kind() != reflect.UnsafePointer {
			continue
		}
		f := innerType.Field(i)
		// Skip the "service" field — cbgo.Service also has a single ptr.
		if strings.Contains(strings.ToLower(f.Name), "service") {
			continue
		}
		if !fv.Field(0).CanAddr() {
			continue
		}
		rawPtr := *(*unsafe.Pointer)(unsafe.Pointer(fv.Field(0).UnsafeAddr()))
		slog.Debug("ble: extractCBCharacteristicPtr: extracted pointer via fallback",
			"fieldIndex", i, "fieldName", f.Name, "ptr", fmt.Sprintf("%p", rawPtr))
		return rawPtr
	}

	slog.Debug("ble: extractCBCharacteristicPtr: no matching field found in inner struct",
		"numFields", numFields)
	return nil
}

// ─── Address / UUID conversion helpers ───────────────────────────────────────

// bleAddressFromTinygo converts a tinygo bluetooth.Address to our BLEAddress.
// On macOS the address is a CoreBluetooth UUID string; on Linux it is a MAC.
func bleAddressFromTinygo(addr ble.Address) BLEAddress {
	return BLEAddress(addr.String())
}

// bleAddressToTinygo converts our BLEAddress back to a tinygo bluetooth.Address.
func bleAddressToTinygo(addr BLEAddress) (ble.Address, error) {
	a := ble.Address{}
	a.Set(string(addr))
	// Set() on macOS calls ParseUUID internally; on Linux it parses the MAC.
	// There is no error return from Set() in tinygo, so we do a round-trip
	// check to detect obviously invalid addresses.
	if a.String() == "" {
		return ble.Address{}, fmt.Errorf("could not parse BLE address %q", addr)
	}
	return a, nil
}

// convertUUIDs converts a slice of our BLEUUID strings to tinygo bluetooth.UUID
// values. Returns an error if any UUID string is malformed.
func convertUUIDs(uuids []BLEUUID) ([]ble.UUID, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	result := make([]ble.UUID, len(uuids))
	for i, u := range uuids {
		parsed, err := ble.ParseUUID(string(u))
		if err != nil {
			return nil, fmt.Errorf("ble: invalid UUID %q: %w", u, err)
		}
		result[i] = parsed
	}
	return result, nil
}
