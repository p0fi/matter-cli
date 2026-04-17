// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func newTestCommissionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("wifi-ssid", "", "")
	cmd.Flags().String("wifi-password", "", "")
	cmd.Flags().String("thread-dataset", "", "")
	return cmd
}

func TestBuildNetworkCreds_FlagTakesPrecedenceOverViper(t *testing.T) {
	viper.Reset()
	viper.Set("wifi.ssid", "config-net")
	viper.Set("wifi.password", "config-pass")
	t.Cleanup(viper.Reset)

	cmd := newTestCommissionCmd()
	_ = cmd.Flags().Set("wifi-ssid", "flag-net")
	_ = cmd.Flags().Set("wifi-password", "flag-pass")

	creds, err := buildNetworkCreds(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("expected credentials, got nil")
	}
	if creds.WiFi == nil {
		t.Fatal("expected WiFi credentials")
	}
	if creds.WiFi.SSID != "flag-net" {
		t.Errorf("SSID = %q, want %q", creds.WiFi.SSID, "flag-net")
	}
	if creds.WiFi.Password != "flag-pass" {
		t.Errorf("password = %q, want %q", creds.WiFi.Password, "flag-pass")
	}
}

func TestBuildNetworkCreds_ViperUsedWhenFlagNotSet(t *testing.T) {
	viper.Reset()
	viper.Set("wifi.ssid", "config-net")
	viper.Set("wifi.password", "config-pass")
	t.Cleanup(viper.Reset)

	cmd := newTestCommissionCmd()

	creds, err := buildNetworkCreds(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("expected credentials, got nil")
	}
	if creds.WiFi == nil {
		t.Fatal("expected WiFi credentials")
	}
	if creds.WiFi.SSID != "config-net" {
		t.Errorf("SSID = %q, want %q", creds.WiFi.SSID, "config-net")
	}
}

func TestBuildNetworkCreds_ExplicitEmptyFlagDoesNotFallback(t *testing.T) {
	viper.Reset()
	viper.Set("wifi.ssid", "config-net")
	viper.Set("wifi.password", "config-pass")
	t.Cleanup(viper.Reset)

	cmd := newTestCommissionCmd()
	// Explicitly set flag to empty string — must NOT fall back to viper.
	_ = cmd.Flags().Set("wifi-ssid", "")

	creds, err := buildNetworkCreds(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// ssid is "" (explicitly set) and password is "" (not set, viper has "config-pass")
	// but ssid="" means no wifi creds are built — result should be nil.
	if creds != nil && creds.WiFi != nil && creds.WiFi.SSID == "config-net" {
		t.Error("explicit empty flag should not fall back to viper value")
	}
}

func TestBuildNetworkCreds_ThreadDatasetFromViper(t *testing.T) {
	viper.Reset()
	// Minimal valid Thread Active Operational Dataset (53 bytes):
	// Channel(0x00) + PAN ID(0x01) + Extended PAN ID(0x02) + Network Name(0x03) +
	// Network Key(0x05) + Active Timestamp(0x0E) — all required TLV types present.
	viper.Set("thread.dataset", "00030000150102face0208deadbeefcafe0001030474657374051000112233445566778899aabbccddeeff0e080000000000010000")
	t.Cleanup(viper.Reset)

	cmd := newTestCommissionCmd()

	creds, err := buildNetworkCreds(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds == nil {
		t.Fatal("expected credentials, got nil")
	}
	if creds.Thread == nil {
		t.Fatal("expected Thread credentials")
	}
}

func TestBuildNetworkCreds_NilWhenNoCredsSet(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := newTestCommissionCmd()

	creds, err := buildNetworkCreds(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != nil {
		t.Errorf("expected nil credentials, got %+v", creds)
	}
}
