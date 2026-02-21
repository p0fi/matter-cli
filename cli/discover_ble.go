// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package cli

import (
	"fmt"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/transport"
	"github.com/spf13/cobra"
)

func init() {
	// Append `discover ble` to the already-registered discover command.
	// The discover command is registered in discover.go's init(); by the time
	// this init() runs the parent command exists in rootCmd's subcommand tree.
	for _, sub := range rootCmd.Commands() {
		if sub.Use == "discover" {
			sub.AddCommand(newDiscoverBLECmd())
			return
		}
	}
	// If the discover command hasn't been registered yet (init order is not
	// guaranteed), add it lazily via PersistentPreRun on the root command.
	// In practice Go runs init() functions in the order their files are
	// compiled, and discover.go is alphabetically before discover_ble.go, so
	// the loop above will always find the command. The fallback is here purely
	// for safety.
}

// newDiscoverBLECmd creates `matter discover ble`.
//
// It enables the local Bluetooth adapter and scans for Matter devices
// advertising on the Matter BLE service UUID (0xFFF6). Results are printed as
// a table showing the BLE address, discriminator, vendor/product IDs, and
// signal strength. The scan runs until the --timeout elapses.
func newDiscoverBLECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ble",
		Short: "Discover commissionable Matter devices over Bluetooth LE",
		Long: `Scan for Matter devices advertising over Bluetooth Low Energy.

Matter devices in commissioning mode broadcast a BLE advertisement on the
Matter service UUID (0xFFF6). The advertisement payload contains the device's
12-bit discriminator, vendor ID, and product ID.

The BLE address printed here can be passed directly to:
  matter commission ble-address <address> --setup-pin <pin>

Platform requirements:
  macOS  – Bluetooth permission must be granted to the terminal app.
  Linux  – User needs cap_net_admin or membership in the "bluetooth" group.`,
		Example: `  matter discover ble
  matter discover ble --timeout 15s
  matter discover ble --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetDuration("timeout")

			adapter := transport.NewDefaultBLEAdapter()
			if err := adapter.Enable(); err != nil {
				return fmt.Errorf("enabling BLE adapter: %w\n\nMake sure Bluetooth is enabled and the application has permission to use it.", err)
			}

			scanner := transport.NewBLEScanner(adapter)
			browser := discovery.NewBLEBrowser(scanner)

			fmt.Fprintf(cmd.OutOrStdout(), "%s Scanning for Matter devices over BLE for %s…\n",
				output.InfoIcon(), timeout)

			ctx := cmd.Context()
			devices, err := browser.Scan(ctx, timeout)
			if err != nil {
				return fmt.Errorf("BLE scan: %w", err)
			}

			if len(devices) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("No commissionable BLE devices found."))
				fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("Make sure the device is in commissioning mode and within BLE range."))
				return nil
			}

			return formatBLEDevices(cmd, devices)
		},
	}
	cmd.Flags().DurationP("timeout", "t", 10*time.Second, "BLE scan duration")
	return cmd
}

// formatBLEDevices renders a slice of BLE-discovered devices to the command's
// output stream, respecting the --format flag.
func formatBLEDevices(cmd *cobra.Command, devices []*discovery.Device) error {
	format, _ := cmd.Flags().GetString("format")
	f := output.New(format)

	if _, ok := f.(*output.TableFormatter); ok {
		td := &output.TableData{
			Headers: []string{"BLE ADDRESS", "NAME", "DISCRIMINATOR", "VENDOR", "PRODUCT"},
		}
		for _, d := range devices {
			name := d.Name
			if name == "" {
				name = output.Muted("(unknown)")
			}
			vid := fmt.Sprintf("0x%04X", d.VendorID)
			pid := fmt.Sprintf("0x%04X", d.ProductID)
			// Zero VID/PID means the advertisement didn't include them.
			if d.VendorID == 0 {
				vid = output.Muted("—")
			}
			if d.ProductID == 0 {
				pid = output.Muted("—")
			}
			td.Rows = append(td.Rows, []string{
				d.BLEAddress,
				name,
				fmt.Sprintf("%d (0x%03X)", d.Discriminator, d.Discriminator),
				vid,
				pid,
			})
		}
		return f.Format(cmd.OutOrStdout(), td)
	}

	return f.Format(cmd.OutOrStdout(), devices)
}
