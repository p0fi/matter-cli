// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
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
}

// newDiscoverBLECmd creates `matter discover ble`.
//
// It enables the local Bluetooth adapter and scans for Matter devices
// advertising on the Matter BLE service UUID (0xFFF6). Results are printed as
// a table showing the BLE address, discriminator, vendor/product IDs, and
// signal strength. The scan runs until the --timeout elapses.
//
// With --raw, every BLE advertisement is printed regardless of whether it is a
// Matter device — useful for diagnosing whether the BLE adapter is scanning at
// all.
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

Use --raw to dump every BLE advertisement regardless of type — helpful when no
Matter devices are found and you want to confirm the BLE adapter is working.

Platform requirements:
  macOS  – Bluetooth permission must be granted to the terminal app.
  Linux  – User needs cap_net_admin or membership in the "bluetooth" group.`,
		Example: `  matter discover ble
  matter discover ble --timeout 15s
  matter discover ble --format json
  matter discover ble --raw`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetDuration("timeout")
			raw, _ := cmd.Flags().GetBool("raw")

			adapter := transport.NewDefaultBLEAdapter()
			if err := adapter.Enable(); err != nil {
				return fmt.Errorf("enabling BLE adapter: %w\n\nMake sure Bluetooth is enabled and the application has permission to use it.", err)
			}

			if raw {
				return runRawBLEScan(cmd, adapter, timeout)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s Scanning for Matter devices over BLE for %s…\n",
				output.InfoIcon(), timeout)

			// Wrap the adapter to count every advertisement so we can give a
			// useful hint when nothing matches (e.g. "N non-Matter devices seen").
			var totalSeen atomic.Int64
			countingAdapter := &countingBLEAdapter{
				BLEAdapter: adapter,
				onAdv:      func() { totalSeen.Add(1) },
			}
			scanner := transport.NewBLEScanner(countingAdapter)
			browser := discovery.NewBLEBrowser(scanner)

			ctx := cmd.Context()
			devices, err := browser.Scan(ctx, timeout)
			if err != nil {
				return fmt.Errorf("BLE scan: %w", err)
			}

			total := totalSeen.Load()
			if len(devices) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("No commissionable Matter devices found."))
				if total > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s %d non-Matter BLE advertisement(s) were seen — try 'matter discover ble --raw' to inspect them.\n",
						output.InfoIcon(), total)
				} else {
					fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("No BLE advertisements were received at all."))
					fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("  • Is Bluetooth enabled?"))
					fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("  • Does this terminal app have Bluetooth permission? (macOS: System Settings → Privacy & Security → Bluetooth)"))
					fmt.Fprintln(cmd.ErrOrStderr(), output.Muted("  • Is the Matter device in commissioning mode and within BLE range?"))
				}
				return nil
			}

			return formatBLEDevices(cmd, devices)
		},
	}
	cmd.Flags().DurationP("timeout", "t", 10*time.Second, "BLE scan duration")
	cmd.Flags().Bool("raw", false, "dump ALL BLE advertisements (not just Matter devices) for diagnostics")
	return cmd
}

// runRawBLEScan scans for all BLE advertisements and prints each one as it
// arrives, regardless of whether it is a Matter device. This is the diagnostic
// mode triggered by --raw.
func runRawBLEScan(cmd *cobra.Command, adapter transport.BLEAdapter, timeout time.Duration) error {
	fmt.Fprintf(cmd.OutOrStdout(), "%s Raw BLE scan — showing ALL advertisements for %s…\n",
		output.InfoIcon(), timeout)
	fmt.Fprintln(cmd.OutOrStdout(), output.Muted("(Press Ctrl-C to stop early)"))
	fmt.Fprintln(cmd.OutOrStdout())

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()

	var mu sync.Mutex
	var count int
	// seen deduplicates by address so we print each device once (with the
	// highest-RSSI observation).
	seen := make(map[transport.BLEAddress]int16) // addr → best RSSI

	err := adapter.Scan(ctx, func(adv transport.BLEScanAdvertisement) {
		mu.Lock()
		defer mu.Unlock()

		// Deduplicate but still count every observation.
		count++
		if prev, dup := seen[adv.Address]; dup {
			if adv.RSSI > prev {
				seen[adv.Address] = adv.RSSI
			}
			return
		}
		seen[adv.Address] = adv.RSSI

		// Print immediately so the user sees devices as they're found.
		name := adv.LocalName
		if name == "" {
			name = output.Muted("(no name)")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  RSSI %d  %s\n",
			output.Accent(string(adv.Address)), adv.RSSI, name)

		// Print service data if present.
		if len(adv.ServiceData) > 0 {
			for uuid, data := range adv.ServiceData {
				isMatter := uuid == transport.MatterServiceUUID
				label := string(uuid)
				if isMatter {
					label = output.Bold("Matter(0xFFF6)")
				}
				if len(data) == 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "      svc-data  %s  (no payload)\n", label)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "      svc-data  %s  %s\n", label, hex.EncodeToString(data))
				}
			}
		}
	})

	// Context cancellation is the normal end of a scan — not an error.
	if err == context.Canceled || err == context.DeadlineExceeded {
		err = nil
	}

	mu.Lock()
	unique := len(seen)
	total := count
	mu.Unlock()

	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintf(cmd.OutOrStdout(), "%s Scan complete: %d unique device(s), %d total observation(s).\n",
		output.InfoIcon(), unique, total)

	return err
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

// ─── countingBLEAdapter ───────────────────────────────────────────────────────

// countingBLEAdapter wraps a BLEAdapter and calls onAdv for every advertisement
// received, regardless of type. It is used to count total advertisements so we
// can distinguish "BLE is working but no Matter devices" from "BLE not working".
type countingBLEAdapter struct {
	transport.BLEAdapter
	onAdv func()
}

// Scan wraps the underlying adapter's Scan and invokes onAdv on every callback.
func (a *countingBLEAdapter) Scan(ctx context.Context, cb func(transport.BLEScanAdvertisement)) error {
	return a.BLEAdapter.Scan(ctx, func(adv transport.BLEScanAdvertisement) {
		a.onAdv()
		cb(adv)
	})
}
