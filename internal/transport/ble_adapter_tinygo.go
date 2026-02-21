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
	"strings"

	ble "tinygo.org/x/bluetooth"
)

// ─── tinygoAdapter ────────────────────────────────────────────────────────────

// tinygoAdapter wraps a *bluetooth.Adapter to implement the bleAdapter
// interface. It translates between the platform-agnostic BLEAddress / BLEUUID
// types used throughout this package and the concrete types exposed by the
// tinygo bluetooth library.
type tinygoAdapter struct {
	a *ble.Adapter
}

// NewDefaultBLEAdapter returns a BLEAdapter backed by the system's default
// Bluetooth adapter (bluetooth.DefaultAdapter). Call Enable() on the returned
// adapter before using it.
func NewDefaultBLEAdapter() BLEAdapter {
	return &tinygoAdapter{a: ble.DefaultAdapter}
}

// Enable initialises the underlying Bluetooth adapter hardware.
func (t *tinygoAdapter) Enable() error {
	return t.a.Enable()
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
		result[i] = &tinygoCharacteristic{c: c}
	}
	return result, nil
}

// ─── tinygoCharacteristic ─────────────────────────────────────────────────────

// tinygoCharacteristic wraps a bluetooth.DeviceCharacteristic to implement
// bleCharacteristic.
type tinygoCharacteristic struct {
	c ble.DeviceCharacteristic
}

// UUID returns the 128-bit UUID of this GATT characteristic.
func (tc *tinygoCharacteristic) UUID() BLEUUID {
	return BLEUUID(tc.c.UUID().String())
}

// Write performs a GATT Write (or Write Without Response) on the characteristic.
func (tc *tinygoCharacteristic) Write(data []byte) (int, error) {
	return tc.c.WriteWithoutResponse(data)
}

// EnableNotifications registers cb to be called on each indication or
// notification received from the device on this characteristic.
func (tc *tinygoCharacteristic) EnableNotifications(cb func(data []byte)) error {
	return tc.c.EnableNotifications(cb)
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
