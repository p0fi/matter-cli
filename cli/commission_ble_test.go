// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package cli

import (
	"context"
	"testing"
)

// ─── staticBLEDiscoverer tests ────────────────────────────────────────────────

func TestStaticBLEDiscoverer_ReturnsAddr(t *testing.T) {
	d := &staticBLEDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF"}

	addr, err := d.DiscoverCommissionable(context.Background(), 0x0ABC, 0)
	if err != nil {
		t.Fatalf("DiscoverCommissionable: unexpected error: %v", err)
	}
	if addr != "ble://AA:BB:CC:DD:EE:FF" {
		t.Errorf("addr = %q, want %q", addr, "ble://AA:BB:CC:DD:EE:FF")
	}
}

func TestStaticBLEDiscoverer_IgnoresDiscriminator(t *testing.T) {
	const wantAddr = "ble://12345678-1234-1234-1234-123456789ABC"
	d := &staticBLEDiscoverer{addr: wantAddr}

	// Different discriminator values must all return the same address.
	for _, disc := range []uint16{0x000, 0x001, 0xABC, 0xFFF} {
		addr, err := d.DiscoverCommissionable(context.Background(), disc, 0)
		if err != nil {
			t.Fatalf("discriminator 0x%03X: unexpected error: %v", disc, err)
		}
		if addr != wantAddr {
			t.Errorf("discriminator 0x%03X: addr = %q, want %q", disc, addr, wantAddr)
		}
	}
}

func TestStaticBLEDiscoverer_ContextCancelled(t *testing.T) {
	d := &staticBLEDiscoverer{addr: "ble://AA:BB:CC:DD:EE:FF"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// staticBLEDiscoverer returns immediately without blocking on context.
	addr, err := d.DiscoverCommissionable(ctx, 0x100, 0)
	if err != nil {
		t.Fatalf("DiscoverCommissionable with cancelled ctx: unexpected error: %v", err)
	}
	if addr != "ble://AA:BB:CC:DD:EE:FF" {
		t.Errorf("addr = %q, want %q", addr, "ble://AA:BB:CC:DD:EE:FF")
	}
}

// ─── resolveOrAllocNodeID tests ───────────────────────────────────────────────

func TestResolveOrAllocNodeID_FromResolvedTarget(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 42, Endpoint: 1, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	got, err := resolveOrAllocNodeID()
	if err != nil {
		t.Fatalf("resolveOrAllocNodeID: unexpected error: %v", err)
	}
	if got != 42 {
		t.Errorf("resolveOrAllocNodeID() = %d, want 42", got)
	}
}

func TestResolveOrAllocNodeID_TargetNodeIDZeroFallsThrough(t *testing.T) {
	prev := resolvedTarget
	// A target with NodeID == 0 should not be used — fall through to nextNodeID.
	resolvedTarget = &Target{NodeID: 0}
	defer func() { resolvedTarget = prev }()

	// nextNodeID opens the store; with no store present it returns 1.
	got, err := resolveOrAllocNodeID()
	if err != nil {
		t.Fatalf("resolveOrAllocNodeID: unexpected error: %v", err)
	}
	if got == 0 {
		t.Error("resolveOrAllocNodeID() returned 0, want >= 1")
	}
}

func TestResolveOrAllocNodeID_NoTarget(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = nil
	defer func() { resolvedTarget = prev }()

	// With no store on disk nextNodeID always returns 1.
	got, err := resolveOrAllocNodeID()
	if err != nil {
		t.Fatalf("resolveOrAllocNodeID: unexpected error: %v", err)
	}
	if got == 0 {
		t.Error("resolveOrAllocNodeID() returned 0, want >= 1")
	}
}

// ─── Command structure tests ──────────────────────────────────────────────────

func TestNewCommissionBLECmd_Structure(t *testing.T) {
	cmd := newCommissionBLECmd()

	if cmd.Use != "ble <setup-code>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "ble <setup-code>")
	}
	if cmd.Short == "" {
		t.Error("Short description must not be empty")
	}
	if cmd.Long == "" {
		t.Error("Long description must not be empty")
	}
	if cmd.Example == "" {
		t.Error("Example must not be empty")
	}
	if cmd.RunE == nil {
		t.Error("RunE must be set")
	}
	// Must accept exactly one positional argument.
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("Args validator: expected error for zero args, got nil")
	}
	if err := cmd.Args(cmd, []string{"MT:foo"}); err != nil {
		t.Errorf("Args validator: unexpected error for one arg: %v", err)
	}
	if err := cmd.Args(cmd, []string{"MT:foo", "extra"}); err == nil {
		t.Error("Args validator: expected error for two args, got nil")
	}
}

func TestNewCommissionBLECmd_Flags(t *testing.T) {
	cmd := newCommissionBLECmd()

	requiredFlags := []string{
		"scan-timeout",
		"wifi-ssid",
		"wifi-password",
		"thread-dataset",
	}
	for _, name := range requiredFlags {
		if f := cmd.Flags().Lookup(name); f == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}

func TestNewCommissionBLEAddressCmd_Structure(t *testing.T) {
	cmd := newCommissionBLEAddressCmd()

	if cmd.Use != "ble-address <ble-address>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "ble-address <ble-address>")
	}
	if cmd.Short == "" {
		t.Error("Short description must not be empty")
	}
	if cmd.Long == "" {
		t.Error("Long description must not be empty")
	}
	if cmd.Example == "" {
		t.Error("Example must not be empty")
	}
	if cmd.RunE == nil {
		t.Error("RunE must be set")
	}
	// Must accept exactly one positional argument.
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("Args validator: expected error for zero args, got nil")
	}
	if err := cmd.Args(cmd, []string{"AA:BB:CC:DD:EE:FF"}); err != nil {
		t.Errorf("Args validator: unexpected error for one arg: %v", err)
	}
	if err := cmd.Args(cmd, []string{"AA:BB:CC:DD:EE:FF", "extra"}); err == nil {
		t.Error("Args validator: expected error for two args, got nil")
	}
}

func TestNewCommissionBLEAddressCmd_Flags(t *testing.T) {
	cmd := newCommissionBLEAddressCmd()

	requiredFlags := []string{
		"setup-pin",
		"wifi-ssid",
		"wifi-password",
		"thread-dataset",
	}
	for _, name := range requiredFlags {
		if f := cmd.Flags().Lookup(name); f == nil {
			t.Errorf("flag --%s not registered", name)
		}
	}
}

func TestAddBLECommissionCommands_AddsSubcommands(t *testing.T) {
	// Build a fresh parent command so we don't pollute the global rootCmd.
	parent := newCommissionCmd()

	subNames := make(map[string]bool)
	for _, sub := range parent.Commands() {
		subNames[sub.Name()] = true
	}

	if !subNames["ble"] {
		t.Error("addBLECommissionCommands did not register 'ble' subcommand")
	}
	if !subNames["ble-address"] {
		t.Error("addBLECommissionCommands did not register 'ble-address' subcommand")
	}
}

func TestNewCommissionBLEAddressCmd_RequiresSetupPin(t *testing.T) {
	cmd := newCommissionBLEAddressCmd()
	// --setup-pin defaults to 0; RunE must reject that.
	// We invoke RunE directly with a dummy arg to test the early-return guard.
	// The function returns before touching the store or BLE when pin == 0.
	err := cmd.RunE(cmd, []string{"AA:BB:CC:DD:EE:FF"})
	if err == nil {
		t.Fatal("RunE: expected error when --setup-pin is not set, got nil")
	}
	if !containsStr(err.Error(), "setup-pin") {
		t.Errorf("RunE: error should mention 'setup-pin', got: %v", err)
	}
}
