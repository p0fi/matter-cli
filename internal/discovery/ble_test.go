// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/transport"
)

// ─── Mock BLE scanner ─────────────────────────────────────────────────────────

// mockBLEScanner is a test double for bleScanner.
type mockBLEScanner struct {
	scanResults []transport.ScanResult
	scanErr     error

	findResult *transport.ScanResult
	findErr    error
}

func (m *mockBLEScanner) Scan(ctx context.Context) ([]transport.ScanResult, error) {
	if m.scanErr != nil {
		return nil, m.scanErr
	}
	return m.scanResults, nil
}

func (m *mockBLEScanner) FindByDiscriminator(ctx context.Context, discriminator uint16) (*transport.ScanResult, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	return m.findResult, nil
}

// Compile-time check.
var _ bleScanner = (*mockBLEScanner)(nil)

// ─── DiscoverCommissionable tests ─────────────────────────────────────────────

func TestBLEBrowser_DiscoverCommissionable_Found(t *testing.T) {
	mock := &mockBLEScanner{
		findResult: &transport.ScanResult{
			Address:       transport.BLEAddress("AA:BB:CC:DD:EE:FF"),
			Discriminator: 0x0ABC,
			VendorID:      0xFFF1,
			ProductID:     0x8001,
			RSSI:          -55,
			Name:          "MatterLight",
		},
	}
	browser := newBLEBrowserWithScanner(mock)

	addr, err := browser.DiscoverCommissionable(context.Background(), 0x0ABC)
	if err != nil {
		t.Fatalf("DiscoverCommissionable: %v", err)
	}
	want := "ble://AA:BB:CC:DD:EE:FF"
	if addr != want {
		t.Errorf("addr = %q, want %q", addr, want)
	}
}

func TestBLEBrowser_DiscoverCommissionable_CoreBluetoothUUID(t *testing.T) {
	// On macOS, BLE addresses are CoreBluetooth UUIDs, not MAC addresses.
	cbUUID := transport.BLEAddress("12345678-1234-1234-1234-123456789ABC")
	mock := &mockBLEScanner{
		findResult: &transport.ScanResult{
			Address:       cbUUID,
			Discriminator: 0x100,
		},
	}
	browser := newBLEBrowserWithScanner(mock)

	addr, err := browser.DiscoverCommissionable(context.Background(), 0x100)
	if err != nil {
		t.Fatalf("DiscoverCommissionable: %v", err)
	}
	want := "ble://12345678-1234-1234-1234-123456789ABC"
	if addr != want {
		t.Errorf("addr = %q, want %q", addr, want)
	}
}

func TestBLEBrowser_DiscoverCommissionable_NotFound(t *testing.T) {
	mock := &mockBLEScanner{
		findErr: errors.New("BLE scan timed out: no device with discriminator 999 found"),
	}
	browser := newBLEBrowserWithScanner(mock)

	_, err := browser.DiscoverCommissionable(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got == "" {
		t.Error("error message should not be empty")
	}
}

func TestBLEBrowser_DiscoverCommissionable_ScanError(t *testing.T) {
	scanErr := errors.New("adapter failed")
	mock := &mockBLEScanner{findErr: scanErr}
	browser := newBLEBrowserWithScanner(mock)

	_, err := browser.DiscoverCommissionable(context.Background(), 0x123)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want wrapped %v", err, scanErr)
	}
}

// ─── Scan tests ───────────────────────────────────────────────────────────────

func TestBLEBrowser_Scan_MultipleDevices(t *testing.T) {
	mock := &mockBLEScanner{
		scanResults: []transport.ScanResult{
			{
				Address:       transport.BLEAddress("AA:BB:CC:DD:EE:01"),
				Discriminator: 0x001,
				VendorID:      0xFFF1,
				ProductID:     0x8000,
				RSSI:          -55,
				Name:          "Light1",
			},
			{
				Address:       transport.BLEAddress("AA:BB:CC:DD:EE:02"),
				Discriminator: 0x002,
				VendorID:      0xFFF2,
				ProductID:     0x8001,
				RSSI:          -70,
				Name:          "Light2",
			},
		},
	}
	browser := newBLEBrowserWithScanner(mock)

	devices, err := browser.Scan(context.Background(), 5*time.Second)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}

	// Check first device.
	d := devices[0]
	if d.Name != "Light1" {
		t.Errorf("Name = %q, want %q", d.Name, "Light1")
	}
	if d.Transport != TransportBLE {
		t.Errorf("Transport = %q, want %q", d.Transport, TransportBLE)
	}
	if d.ServiceType != ServiceCommissionable {
		t.Errorf("ServiceType = %q, want %q", d.ServiceType, ServiceCommissionable)
	}
	if d.Discriminator != 0x001 {
		t.Errorf("Discriminator = %d, want %d", d.Discriminator, 0x001)
	}
	if d.VendorID != 0xFFF1 {
		t.Errorf("VendorID = %d, want %d", d.VendorID, 0xFFF1)
	}
	if d.ProductID != 0x8000 {
		t.Errorf("ProductID = %d, want %d", d.ProductID, 0x8000)
	}
	if d.BLEAddress != "AA:BB:CC:DD:EE:01" {
		t.Errorf("BLEAddress = %q, want %q", d.BLEAddress, "AA:BB:CC:DD:EE:01")
	}

	// IP-specific fields should be empty for BLE devices.
	if len(d.IPs) != 0 {
		t.Errorf("IPs should be empty for BLE device, got %v", d.IPs)
	}
	if d.Port != 0 {
		t.Errorf("Port should be 0 for BLE device, got %d", d.Port)
	}
	if d.Host != "" {
		t.Errorf("Host should be empty for BLE device, got %q", d.Host)
	}

	// Check second device.
	d2 := devices[1]
	if d2.Discriminator != 0x002 {
		t.Errorf("Device 2 Discriminator = %d, want %d", d2.Discriminator, 0x002)
	}
	if d2.BLEAddress != "AA:BB:CC:DD:EE:02" {
		t.Errorf("Device 2 BLEAddress = %q, want %q", d2.BLEAddress, "AA:BB:CC:DD:EE:02")
	}
}

func TestBLEBrowser_Scan_NoDevices(t *testing.T) {
	mock := &mockBLEScanner{scanResults: nil}
	browser := newBLEBrowserWithScanner(mock)

	devices, err := browser.Scan(context.Background(), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("got %d devices, want 0", len(devices))
	}
}

func TestBLEBrowser_Scan_Error(t *testing.T) {
	scanErr := errors.New("adapter failed")
	mock := &mockBLEScanner{scanErr: scanErr}
	browser := newBLEBrowserWithScanner(mock)

	_, err := browser.Scan(context.Background(), 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want wrapped %v", err, scanErr)
	}
}

// ─── scanResultToDevice tests ─────────────────────────────────────────────────

func TestScanResultToDevice(t *testing.T) {
	r := transport.ScanResult{
		Address:       transport.BLEAddress("AA:BB:CC:DD:EE:FF"),
		Discriminator: 0x0ABC,
		VendorID:      0xFFF1,
		ProductID:     0x8001,
		RSSI:          -60,
		Name:          "TestDevice",
	}

	d := scanResultToDevice(r)

	if d.Name != "TestDevice" {
		t.Errorf("Name = %q, want %q", d.Name, "TestDevice")
	}
	if d.Transport != TransportBLE {
		t.Errorf("Transport = %q, want %q", d.Transport, TransportBLE)
	}
	if d.ServiceType != ServiceCommissionable {
		t.Errorf("ServiceType = %q, want %q", d.ServiceType, ServiceCommissionable)
	}
	if d.Discriminator != 0x0ABC {
		t.Errorf("Discriminator = %d, want %d", d.Discriminator, 0x0ABC)
	}
	if d.VendorID != 0xFFF1 {
		t.Errorf("VendorID = %d, want %d", d.VendorID, 0xFFF1)
	}
	if d.ProductID != 0x8001 {
		t.Errorf("ProductID = %d, want %d", d.ProductID, 0x8001)
	}
	if d.BLEAddress != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("BLEAddress = %q, want %q", d.BLEAddress, "AA:BB:CC:DD:EE:FF")
	}
	// IP fields should be zero-valued.
	if d.Host != "" {
		t.Errorf("Host = %q, want empty", d.Host)
	}
	if d.Port != 0 {
		t.Errorf("Port = %d, want 0", d.Port)
	}
	if len(d.IPs) != 0 {
		t.Errorf("IPs = %v, want nil/empty", d.IPs)
	}
	if d.TXTRecords != nil {
		t.Errorf("TXTRecords = %v, want nil", d.TXTRecords)
	}
}

// ─── Interface compliance ─────────────────────────────────────────────────────

func TestBLEBrowserImplementsDeviceDiscoverer(t *testing.T) {
	// Compile-time check that BLEBrowser satisfies commissioning.DeviceDiscoverer.
	// (We inline the interface to avoid an import cycle.)
	type deviceDiscoverer interface {
		DiscoverCommissionable(ctx context.Context, discriminator uint16) (addr string, err error)
	}
	var _ deviceDiscoverer = (*BLEBrowser)(nil)
}
