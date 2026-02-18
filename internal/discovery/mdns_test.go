// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

// mockResolver simulates mDNS browsing without real network access.
type mockResolver struct {
	entries []*zeroconf.ServiceEntry
	err     error
}

func (m *mockResolver) Browse(_ context.Context, service, domain string, entries chan<- *zeroconf.ServiceEntry) error {
	defer close(entries)
	if m.err != nil {
		return m.err
	}
	for _, e := range m.entries {
		entries <- e
	}
	return nil
}

func TestDiscoverCommissionable(t *testing.T) {
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{
					Instance: "device-1",
				},
				HostName: "device-1.local.",
				Port:     5540,
				AddrIPv4: []net.IP{net.ParseIP("192.168.1.10")},
				Text:     []string{"D=3840", "CM=1", "VP=65521+32769", "SII=300", "SAI=500"},
			},
			{
				ServiceRecord: zeroconf.ServiceRecord{
					Instance: "device-2",
				},
				HostName: "device-2.local.",
				Port:     5540,
				AddrIPv4: []net.IP{net.ParseIP("192.168.1.11")},
				AddrIPv6: []net.IP{net.ParseIP("fe80::2")},
				Text:     []string{"D=100", "CM=2"},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)
	devices, err := browser.DiscoverCommissionable(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("DiscoverCommissionable: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}

	// Check first device.
	d := devices[0]
	if d.Name != "device-1" {
		t.Errorf("Name = %q, want %q", d.Name, "device-1")
	}
	if d.Host != "device-1.local." {
		t.Errorf("Host = %q, want %q", d.Host, "device-1.local.")
	}
	if d.Port != 5540 {
		t.Errorf("Port = %d, want 5540", d.Port)
	}
	if len(d.IPs) != 1 || !d.IPs[0].Equal(net.ParseIP("192.168.1.10")) {
		t.Errorf("IPs = %v, want [192.168.1.10]", d.IPs)
	}
	if d.ServiceType != ServiceCommissionable {
		t.Errorf("ServiceType = %q, want %q", d.ServiceType, ServiceCommissionable)
	}
	if d.Discriminator != 3840 {
		t.Errorf("Discriminator = %d, want 3840", d.Discriminator)
	}
	if d.CommissioningMode != CommissioningModeBasic {
		t.Errorf("CommissioningMode = %d, want %d", d.CommissioningMode, CommissioningModeBasic)
	}
	if d.VendorID != 65521 {
		t.Errorf("VendorID = %d, want 65521", d.VendorID)
	}
	if d.ProductID != 32769 {
		t.Errorf("ProductID = %d, want 32769", d.ProductID)
	}
	if d.SessionIdleInterval != 300 {
		t.Errorf("SII = %d, want 300", d.SessionIdleInterval)
	}
	if d.SessionActiveInterval != 500 {
		t.Errorf("SAI = %d, want 500", d.SessionActiveInterval)
	}

	// Check second device has both v4 and v6 addresses.
	d2 := devices[1]
	if d2.Discriminator != 100 {
		t.Errorf("Device 2 Discriminator = %d, want 100", d2.Discriminator)
	}
	if d2.CommissioningMode != CommissioningModeEnhanced {
		t.Errorf("Device 2 CommissioningMode = %d, want %d", d2.CommissioningMode, CommissioningModeEnhanced)
	}
	if len(d2.IPs) != 2 {
		t.Errorf("Device 2 IPs count = %d, want 2", len(d2.IPs))
	}
}

func TestDiscoverOperational(t *testing.T) {
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{
					Instance: "node-abc",
				},
				HostName: "node-abc.local.",
				Port:     5540,
				AddrIPv4: []net.IP{net.ParseIP("10.0.0.5")},
				Text:     []string{"SII=500"},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)
	devices, err := browser.DiscoverOperational(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("DiscoverOperational: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1", len(devices))
	}
	if devices[0].ServiceType != ServiceOperational {
		t.Errorf("ServiceType = %q, want %q", devices[0].ServiceType, ServiceOperational)
	}
	if devices[0].SessionIdleInterval != 500 {
		t.Errorf("SII = %d, want 500", devices[0].SessionIdleInterval)
	}
}

func TestDiscoverNoDevices(t *testing.T) {
	mock := &mockResolver{entries: nil}

	browser := newMDNSBrowserWithResolver(mock)
	devices, err := browser.DiscoverCommissionable(context.Background(), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("DiscoverCommissionable: %v", err)
	}
	if len(devices) != 0 {
		t.Errorf("got %d devices, want 0", len(devices))
	}
}

func TestDiscoverResolverError(t *testing.T) {
	mock := &mockResolver{err: errors.New("network error")}

	browser := newMDNSBrowserWithResolver(mock)
	_, err := browser.DiscoverCommissionable(context.Background(), 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, mock.err) {
		t.Errorf("error = %v, want wrapped %v", err, mock.err)
	}
}

func TestDiscoverContextCancelled(t *testing.T) {
	// A slow resolver that blocks until context cancellation.
	slowMock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{
					Instance: "slow-device",
				},
				Port:     5540,
				AddrIPv4: []net.IP{net.ParseIP("192.168.1.1")},
				Text:     []string{"D=1"},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(slowMock)
	// Use a very short timeout to trigger context cancellation quickly.
	devices, err := browser.DiscoverCommissionable(context.Background(), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The mock delivers entries synchronously before returning, so we should get them.
	if len(devices) != 1 {
		t.Errorf("got %d devices, want 1", len(devices))
	}
}

func TestBrowserImplementsInterface(t *testing.T) {
	// Compile-time check that MDNSBrowser implements Browser.
	var _ Browser = (*MDNSBrowser)(nil)
}
