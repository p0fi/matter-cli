// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/p0fi/matter-cli/internal/transport"
)

// ─── Address helpers tests ────────────────────────────────────────────────────

func TestIsBLEAddress(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"ble://AA:BB:CC:DD:EE:FF", true},
		{"ble://12345678-1234-1234-1234-123456789ABC", true},
		{"ble://", true},
		{"192.168.1.42:5540", false},
		{"", false},
		{"BLE://uppercase", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := isBLEAddress(tt.addr); got != tt.want {
				t.Errorf("isBLEAddress(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestParseBLEAddress(t *testing.T) {
	tests := []struct {
		addr    string
		want    transport.BLEAddress
		wantErr bool
	}{
		{"ble://AA:BB:CC:DD:EE:FF", "AA:BB:CC:DD:EE:FF", false},
		{"ble://12345678-1234-1234-1234-123456789ABC", "12345678-1234-1234-1234-123456789ABC", false},
		{"ble://", "", false},
		{"192.168.1.42:5540", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got, err := parseBLEAddress(tt.addr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseBLEAddress(%q): want error, got nil", tt.addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBLEAddress(%q): %v", tt.addr, err)
			}
			if got != tt.want {
				t.Errorf("parseBLEAddress(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

// ─── TransportPreference tests ────────────────────────────────────────────────

func TestNewCommissionerWithTransport_IP(t *testing.T) {
	// IP mode should return a commissioner without needing a BLE adapter.
	ctrl, err := NewWithConn(Config{FabricID: 1}, &pipeConn{
		send:   make(chan pipeMsg, 1),
		recv:   make(chan pipeMsg, 1),
		closed: make(chan struct{}),
		myAddr: &mockAddr{network: "pipe", address: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	comm := ctrl.NewCommissionerWithTransport(TransportIP, nil)
	if comm == nil {
		t.Fatal("expected non-nil commissioner")
	}
	if comm.Discoverer == nil {
		t.Error("expected non-nil discoverer")
	}
	if comm.Sessions == nil {
		t.Error("expected non-nil session establisher")
	}
}

func TestNewCommissionerWithTransport_BLE(t *testing.T) {
	ctrl, err := NewWithConn(Config{FabricID: 1}, &pipeConn{
		send:   make(chan pipeMsg, 1),
		recv:   make(chan pipeMsg, 1),
		closed: make(chan struct{}),
		myAddr: &mockAddr{network: "pipe", address: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	adapter := &mockBLEAdapterForController{}
	comm := ctrl.NewCommissionerWithTransport(TransportBLE, adapter)
	if comm == nil {
		t.Fatal("expected non-nil commissioner")
	}
}

func TestNewCommissionerWithTransport_Auto(t *testing.T) {
	ctrl, err := NewWithConn(Config{FabricID: 1}, &pipeConn{
		send:   make(chan pipeMsg, 1),
		recv:   make(chan pipeMsg, 1),
		closed: make(chan struct{}),
		myAddr: &mockAddr{network: "pipe", address: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	adapter := &mockBLEAdapterForController{}
	comm := ctrl.NewCommissionerWithTransport(TransportAuto, adapter)
	if comm == nil {
		t.Fatal("expected non-nil commissioner")
	}
}

// ─── bleSessionEstablisher tests ──────────────────────────────────────────────

func TestBLESessionEstablisher_EstablishPASE_InvalidAddress(t *testing.T) {
	ctrl, err := NewWithConn(Config{FabricID: 1}, &pipeConn{
		send:   make(chan pipeMsg, 1),
		recv:   make(chan pipeMsg, 1),
		closed: make(chan struct{}),
		myAddr: &mockAddr{network: "pipe", address: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()

	adapter := &mockBLEAdapterForController{}
	est := &bleSessionEstablisher{ctrl: ctrl, adapter: adapter}

	// Non-BLE address should fail.
	_, err = est.EstablishPASE(context.Background(), "192.168.1.1:5540", 20202021)
	if err == nil {
		t.Fatal("expected error for non-BLE address")
	}
}

// ─── Mock BLE adapter for controller tests ────────────────────────────────────

// mockBLEAdapterForController implements transport.BLEAdapter for controller-level
// tests. It's minimal — just enough to satisfy the interface.
type mockBLEAdapterForController struct{}

func (m *mockBLEAdapterForController) Enable() error { return nil }

func (m *mockBLEAdapterForController) Scan(ctx context.Context, cb func(transport.BLEScanAdvertisement)) error {
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockBLEAdapterForController) StopScan() error { return nil }

func (m *mockBLEAdapterForController) Connect(ctx context.Context, addr transport.BLEAddress) (transport.BLEDevice, error) {
	return nil, fmt.Errorf("mock: not implemented")
}
