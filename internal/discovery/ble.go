// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/transport"
)

// bleScanner abstracts the BLE scanning operations used by BLEBrowser. The
// production implementation is *transport.BLEScanner; tests provide a mock.
type bleScanner interface {
	Scan(ctx context.Context) ([]transport.ScanResult, error)
	FindByDiscriminator(ctx context.Context, discriminator uint16) (*transport.ScanResult, error)
}

// BLEBrowser discovers commissionable Matter devices over Bluetooth Low Energy.
// It wraps a bleScanner (typically *transport.BLEScanner) and implements the
// commissioning.DeviceDiscoverer interface so it can be used as a drop-in
// replacement for mDNS discovery.
type BLEBrowser struct {
	scanner bleScanner
}

// NewBLEBrowser creates a new BLEBrowser backed by the given BLE scanner.
// The scanner must not be nil.
func NewBLEBrowser(scanner *transport.BLEScanner) *BLEBrowser {
	return &BLEBrowser{scanner: scanner}
}

// newBLEBrowserWithScanner creates a BLEBrowser with a custom scanner (for testing).
func newBLEBrowserWithScanner(scanner bleScanner) *BLEBrowser {
	return &BLEBrowser{scanner: scanner}
}

// DiscoverCommissionable finds a commissionable Matter device with the given
// 12-bit discriminator over BLE. It returns an address string of the form
// "ble://<address>" that can be passed to a BLE-aware SessionEstablisher.
//
// This method satisfies the commissioning.DeviceDiscoverer interface.
func (b *BLEBrowser) DiscoverCommissionable(ctx context.Context, discriminator uint16, _ commissioning.DiscoveryCapabilities) (string, error) {
	result, err := b.scanner.FindByDiscriminator(ctx, discriminator)
	if err != nil {
		return "", fmt.Errorf("BLE discovery: %w", err)
	}
	return "ble://" + result.Address.String(), nil
}

// Scan scans for all commissionable Matter devices over BLE within the given
// timeout and returns them as discovery.Device values with Transport set to
// TransportBLE.
func (b *BLEBrowser) Scan(ctx context.Context, timeout time.Duration) ([]*Device, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results, err := b.scanner.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("BLE scan: %w", err)
	}

	devices := make([]*Device, 0, len(results))
	for _, r := range results {
		devices = append(devices, scanResultToDevice(r))
	}
	return devices, nil
}

// scanResultToDevice converts a transport.ScanResult to a discovery.Device.
func scanResultToDevice(r transport.ScanResult) *Device {
	return &Device{
		Name:          r.Name,
		Transport:     TransportBLE,
		ServiceType:   ServiceCommissionable,
		Discriminator: r.Discriminator,
		VendorID:      r.VendorID,
		ProductID:     r.ProductID,
		BLEAddress:    r.Address.String(),
	}
}
