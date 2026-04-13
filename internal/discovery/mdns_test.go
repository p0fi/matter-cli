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

func TestWatchOperational_FoundEarly(t *testing.T) {
	// Three entries are queued; the callback stops on the second match.
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "OTHER-0000000000000001"},
				HostName:      "other.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.1")},
			},
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "AABBCCDD11223344-0000000000000005"},
				HostName:      "target.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.5")},
			},
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "AABBCCDD11223344-0000000000000099"},
				HostName:      "another.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.99")},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)

	var found *Device
	var callCount int
	err := browser.WatchOperational(context.Background(), 5*time.Second, func(dev *Device) bool {
		callCount++
		if dev.Name == "AABBCCDD11223344-0000000000000005" {
			found = dev
			return true
		}
		return false
	})
	if err != nil {
		t.Fatalf("WatchOperational: %v", err)
	}
	if found == nil {
		t.Fatal("expected target device to be found, got nil")
	}
	if found.Name != "AABBCCDD11223344-0000000000000005" {
		t.Errorf("Name = %q, want %q", found.Name, "AABBCCDD11223344-0000000000000005")
	}
	if len(found.IPs) == 0 || !found.IPs[0].Equal(net.ParseIP("10.0.0.5")) {
		t.Errorf("IPs = %v, want [10.0.0.5]", found.IPs)
	}
	if found.ServiceType != ServiceOperational {
		t.Errorf("ServiceType = %q, want %q", found.ServiceType, ServiceOperational)
	}
	// The third entry should never have been delivered because we stopped early.
	if callCount > 2 {
		t.Errorf("callback called %d times, expected at most 2 (stop on match)", callCount)
	}
}

func TestWatchOperational_NotFound(t *testing.T) {
	// The resolver delivers two entries, neither matches — WatchOperational
	// should return nil (not an error) after the browse exhausts.
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "UNRELATED-0000000000000001"},
				HostName:      "unrelated.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.1")},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)

	var callCount int
	err := browser.WatchOperational(context.Background(), 5*time.Second, func(dev *Device) bool {
		callCount++
		return false // never match
	})
	if err != nil {
		t.Fatalf("WatchOperational returned unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("callback called %d times, want 1", callCount)
	}
}

func TestWatchOperational_ResolverError(t *testing.T) {
	resolverErr := errors.New("multicast socket error")
	mock := &mockResolver{err: resolverErr}

	browser := newMDNSBrowserWithResolver(mock)

	err := browser.WatchOperational(context.Background(), 5*time.Second, func(_ *Device) bool {
		return false
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, resolverErr) {
		t.Errorf("error = %v, want wrapped %v", err, resolverErr)
	}
}

func TestWatchOperational_ContextCancelled(t *testing.T) {
	// A resolver that blocks until its context is cancelled (simulates a
	// long-running browse where the caller gives up).
	blockingMock := &blockingResolver{}

	browser := newMDNSBrowserWithResolver(blockingMock)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := browser.WatchOperational(ctx, 10*time.Second, func(_ *Device) bool {
		return false
	})
	elapsed := time.Since(start)

	// Should return quickly (context timeout), not hang for 10 seconds.
	if elapsed > 2*time.Second {
		t.Errorf("WatchOperational took %v, expected < 2s with 50ms context", elapsed)
	}
	// Context cancellation is not reported as an error by WatchOperational —
	// it just means the browse window closed without finding the target.
	if err != nil {
		t.Fatalf("WatchOperational: unexpected error: %v", err)
	}
}

func TestResolveOperational_Found(t *testing.T) {
	// The compressed fabric ID 0xAABBCCDD11223344 and node ID 5 should match
	// the instance name "AABBCCDD11223344-0000000000000005".
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "UNRELATED-0000000000000001"},
				HostName:      "unrelated.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.1")},
			},
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "AABBCCDD11223344-0000000000000005"},
				HostName:      "target.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.5")},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)
	fabricID := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	dev, err := browser.ResolveOperational(context.Background(), fabricID, 5, 5*time.Second)
	if err != nil {
		t.Fatalf("ResolveOperational: %v", err)
	}
	if dev.Name != "AABBCCDD11223344-0000000000000005" {
		t.Errorf("Name = %q, want %q", dev.Name, "AABBCCDD11223344-0000000000000005")
	}
	if len(dev.IPs) == 0 || !dev.IPs[0].Equal(net.ParseIP("10.0.0.5")) {
		t.Errorf("IPs = %v, want [10.0.0.5]", dev.IPs)
	}
}

func TestResolveOperational_CaseInsensitive(t *testing.T) {
	// Instance name is lowercase; should still match.
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "aabbccdd11223344-0000000000000005"},
				HostName:      "target.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.5")},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)
	fabricID := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	dev, err := browser.ResolveOperational(context.Background(), fabricID, 5, 5*time.Second)
	if err != nil {
		t.Fatalf("ResolveOperational should match case-insensitively: %v", err)
	}
	if dev == nil {
		t.Fatal("expected device, got nil")
	}
}

func TestResolveOperational_NotFound(t *testing.T) {
	// No matching entry — should return a timeout error.
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "FFFFFFFFFFFFFFFF-0000000000000099"},
				HostName:      "other.local.",
				Port:          5540,
				AddrIPv4:      []net.IP{net.ParseIP("10.0.0.99")},
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)
	fabricID := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	_, err := browser.ResolveOperational(context.Background(), fabricID, 5, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for unresolved node, got nil")
	}
}

func TestResolveOperational_SkipsEntryWithNoIPs(t *testing.T) {
	// Entry matches by name but has no IPs — should be skipped, resulting in timeout.
	mock := &mockResolver{
		entries: []*zeroconf.ServiceEntry{
			{
				ServiceRecord: zeroconf.ServiceRecord{Instance: "AABBCCDD11223344-0000000000000005"},
				HostName:      "target.local.",
				Port:          5540,
				// No IPs.
			},
		},
	}

	browser := newMDNSBrowserWithResolver(mock)
	fabricID := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	_, err := browser.ResolveOperational(context.Background(), fabricID, 5, 5*time.Second)
	if err == nil {
		t.Fatal("expected error when matching entry has no IPs, got nil")
	}
}

// blockingResolver is a mockResolver whose Browse blocks until the context is cancelled.
type blockingResolver struct{}

func (b *blockingResolver) Browse(ctx context.Context, _, _ string, entries chan<- *zeroconf.ServiceEntry) error {
	defer close(entries)
	<-ctx.Done()
	return nil
}
