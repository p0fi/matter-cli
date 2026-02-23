// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/discovery"
	"github.com/p0fi/matter-cli/internal/vendordb"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(withGroup(newDiscoverCmd(), groupDevices))
}

// newDiscoverCmd creates the `matter-cli discover` subcommand group.
func newDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover Matter devices on the local network",
	}
	cmd.AddCommand(newDiscoverCommissionableCmd())
	cmd.AddCommand(newDiscoverOperationalCmd())
	return cmd
}

func newDiscoverCommissionableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commissionable",
		Short: "Discover commissionable Matter devices",
		Example: `  matter discover commissionable
  matter discover commissionable --timeout 10s`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetDuration("timeout")
			verbose, _ := cmd.Flags().GetBool("verbose")
			stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

			stepper.Step(fmt.Sprintf("Scanning for commissionable Matter devices for %s…", timeout))

			browser := discovery.NewMDNSBrowser()
			devices, err := browser.DiscoverCommissionable(cmd.Context(), timeout)
			if err != nil {
				stepper.Fail(fmt.Sprintf("Discovery failed: %v", err))
				return fmt.Errorf("discovering commissionable devices: %w", err)
			}
			if len(devices) == 0 {
				stepper.Fail("No commissionable devices found")
				return nil
			}
			stepper.Success(fmt.Sprintf("Found %d commissionable device(s)", len(devices)))
			return formatDevices(cmd, devices)
		},
	}
	cmd.Flags().DurationP("timeout", "t", 5*time.Second, "discovery timeout")
	return cmd
}

func newDiscoverOperationalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operational",
		Short: "Discover operational Matter devices",
		Example: `  matter discover operational
  matter discover operational --timeout 10s`,
		RunE: func(cmd *cobra.Command, args []string) error {
			timeout, _ := cmd.Flags().GetDuration("timeout")
			verbose, _ := cmd.Flags().GetBool("verbose")
			stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

			stepper.Step(fmt.Sprintf("Scanning for operational Matter devices for %s…", timeout))

			browser := discovery.NewMDNSBrowser()
			devices, err := browser.DiscoverOperational(cmd.Context(), timeout)
			if err != nil {
				stepper.Fail(fmt.Sprintf("Discovery failed: %v", err))
				return fmt.Errorf("discovering operational devices: %w", err)
			}
			if len(devices) == 0 {
				stepper.Fail("No operational devices found")
				return nil
			}
			stepper.Success(fmt.Sprintf("Found %d operational device(s)", len(devices)))
			return formatDevices(cmd, devices)
		},
	}
	cmd.Flags().DurationP("timeout", "t", 5*time.Second, "discovery timeout")
	return cmd
}

func formatDevices(cmd *cobra.Command, devices []*discovery.Device) error {
	format, _ := cmd.Flags().GetString("format")
	f := output.New(format)

	if _, ok := f.(*output.TableFormatter); ok {
		td := &output.TableData{
			Headers: []string{"NAME", "ADDRESS", "PORT", "DISCRIMINATOR", "VENDOR", "PRODUCT", "CM"},
		}
		for _, d := range devices {
			var addrs []string
			for _, ip := range d.IPs {
				addrs = append(addrs, ip.String())
			}
			td.Rows = append(td.Rows, []string{
				d.Name,
				strings.Join(addrs, ", "),
				fmt.Sprintf("%d", d.Port),
				fmt.Sprintf("%d", d.Discriminator),
				vendordb.FormatVendorID(d.VendorID),
				fmt.Sprintf("0x%04X", d.ProductID),
				fmt.Sprintf("%d", d.CommissioningMode),
			})
		}
		return f.Format(cmd.OutOrStdout(), td)
	}
	return f.Format(cmd.OutOrStdout(), devices)
}
