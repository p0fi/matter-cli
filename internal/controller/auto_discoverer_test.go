// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/transport"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

type mockTransportDiscoverer struct {
	addr  string
	err   error
	delay time.Duration // simulates slow discovery
}

func (m *mockTransportDiscoverer) DiscoverCommissionable(ctx context.Context, _ uint16, _ commissioning.DiscoveryCapabilities) (string, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return m.addr, m.err
}

type mockBLEAdapter struct {
	enableCalled bool
	enableErr    error
}

func (m *mockBLEAdapter) Enable() error {
	m.enableCalled = true
	return m.enableErr
}
func (m *mockBLEAdapter) Scan(ctx context.Context, _ func(transport.BLEScanAdvertisement)) error {
	<-ctx.Done()
	return ctx.Err()
}
func (m *mockBLEAdapter) StopScan() error { return nil }
func (m *mockBLEAdapter) Connect(_ context.Context, _ transport.BLEAddress) (transport.BLEDevice, error) {
	return nil, errors.New("mock: not implemented")
}

// ─── DiscoveryCapabilities routing tests ─────────────────────────────────────

func TestAutoDiscoverer_SkipsBLEWhenNotInCaps(t *testing.T) {
	adapter := &mockBLEAdapter{}
	mdns := &mockTransportDiscoverer{addr: "192.168.1.1:5540"}

	d := &autoDiscoverer{
		ble:     &mockTransportDiscoverer{err: errors.New("should not be called")},
		mdns:    mdns,
		adapter: adapter,
	}

	addr, err := d.DiscoverCommissionable(context.Background(), 3840, commissioning.DiscoveryOnNetwork)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != mdns.addr {
		t.Errorf("addr = %q, want %q", addr, mdns.addr)
	}
	if adapter.enableCalled {
		t.Error("adapter.Enable() was called, want skipped")
	}
}

func TestAutoDiscoverer_SkipsMDNSWhenNotInCaps(t *testing.T) {
	adapter := &mockBLEAdapter{}
	ble := &mockTransportDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF"}

	d := &autoDiscoverer{
		ble:     ble,
		mdns:    &mockTransportDiscoverer{err: errors.New("should not be called")},
		adapter: adapter,
	}

	addr, err := d.DiscoverCommissionable(context.Background(), 3840, commissioning.DiscoveryBLE)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != ble.addr {
		t.Errorf("addr = %q, want %q", addr, ble.addr)
	}
}

// ─── Parallel path tests ──────────────────────────────────────────────────────

func TestAutoDiscoverer_MDNSWinsAndCancelsBLE(t *testing.T) {
	adapter := &mockBLEAdapter{}

	// BLE is slow; mDNS responds immediately.
	d := &autoDiscoverer{
		ble:     &mockTransportDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF", delay: 10 * time.Second},
		mdns:    &mockTransportDiscoverer{addr: "192.168.1.1:5540"},
		adapter: adapter,
	}

	start := time.Now()
	addr, err := d.DiscoverCommissionable(context.Background(), 3840, 0)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "192.168.1.1:5540" {
		t.Errorf("addr = %q, want mDNS address", addr)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v, want BLE goroutine cancelled promptly", elapsed)
	}
}

func TestAutoDiscoverer_BLEWinsAndCancelsMDNS(t *testing.T) {
	adapter := &mockBLEAdapter{}

	// mDNS is slow; BLE responds immediately.
	d := &autoDiscoverer{
		ble:     &mockTransportDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF"},
		mdns:    &mockTransportDiscoverer{addr: "192.168.1.1:5540", delay: 10 * time.Second},
		adapter: adapter,
	}

	start := time.Now()
	addr, err := d.DiscoverCommissionable(context.Background(), 3840, 0)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "ble://AA:BB:CC:DD:EE:FF" {
		t.Errorf("addr = %q, want BLE address", addr)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v, want mDNS goroutine cancelled promptly", elapsed)
	}
}

func TestAutoDiscoverer_BothFailReturnsMDNSError(t *testing.T) {
	adapter := &mockBLEAdapter{}
	bleErr := errors.New("BLE scan timed out")
	mdnsErr := errors.New("no commissionable device found")

	d := &autoDiscoverer{
		ble:     &mockTransportDiscoverer{err: bleErr},
		mdns:    &mockTransportDiscoverer{err: mdnsErr},
		adapter: adapter,
	}

	_, err := d.DiscoverCommissionable(context.Background(), 3840, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, mdnsErr) {
		t.Errorf("err = %v, want mDNS error %v", err, mdnsErr)
	}
}

func TestAutoDiscoverer_BLEEnableFailureFallsThrough(t *testing.T) {
	adapter := &mockBLEAdapter{enableErr: errors.New("no bluetooth hardware")}

	d := &autoDiscoverer{
		ble:     &mockTransportDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF"},
		mdns:    &mockTransportDiscoverer{addr: "192.168.1.1:5540"},
		adapter: adapter,
	}

	// BLE adapter fails to enable; mDNS should still succeed.
	addr, err := d.DiscoverCommissionable(context.Background(), 3840, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != "192.168.1.1:5540" {
		t.Errorf("addr = %q, want mDNS address", addr)
	}
}

func TestAutoDiscoverer_UnknownCapsTriesBoth(t *testing.T) {
	adapter := &mockBLEAdapter{}

	// Give mDNS a small delay so the BLE goroutine has time to call
	// Enable() before mDNS returns and cancels the context.
	d := &autoDiscoverer{
		ble:     &mockTransportDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF", delay: 10 * time.Second},
		mdns:    &mockTransportDiscoverer{addr: "192.168.1.1:5540", delay: 50 * time.Millisecond},
		adapter: adapter,
	}

	// caps = 0 means unknown: both transports must be tried.
	addr, err := d.DiscoverCommissionable(context.Background(), 3840, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr == "" {
		t.Error("expected a result from one of the transports")
	}
	if !adapter.enableCalled {
		t.Error("adapter.Enable() not called; BLE goroutine should have started")
	}
}
