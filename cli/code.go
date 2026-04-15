// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"strings"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/vendordb"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(withGroup(newCodeCmd(), groupTools))
}

// newCodeCmd creates the `matter code` subcommand group.
func newCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code",
		Short: "Parse or generate Matter setup codes (QR codes, manual pairing codes)",
	}
	cmd.AddCommand(newCodeParseCmd())
	cmd.AddCommand(newCodeGenerateCmd())
	return cmd
}

// formatCommissioningFlow returns a human-readable string for a CommissioningFlow value.
func formatCommissioningFlow(f commissioning.CommissioningFlow) string {
	switch f {
	case commissioning.FlowStandard:
		return fmt.Sprintf("Standard (%d)", f)
	case commissioning.FlowUserIntent:
		return fmt.Sprintf("User-Intent (%d)", f)
	case commissioning.FlowCustom:
		return fmt.Sprintf("Custom (%d)", f)
	default:
		return fmt.Sprintf("Unknown (%d)", f)
	}
}

// formatDiscoveryCapabilities returns a human-readable string listing all set
// discovery capability flags together with the raw hex value.
func formatDiscoveryCapabilities(d commissioning.DiscoveryCapabilities) string {
	type flag struct {
		bit  commissioning.DiscoveryCapabilities
		name string
	}
	flags := []flag{
		{commissioning.DiscoverySoftAP, "SoftAP"},
		{commissioning.DiscoveryBLE, "BLE"},
		{commissioning.DiscoveryOnNetwork, "OnNetwork"},
	}

	var names []string
	for _, f := range flags {
		if d&f.bit != 0 {
			names = append(names, f.name)
		}
	}

	if len(names) == 0 {
		return fmt.Sprintf("None (0x%02X)", d)
	}
	return fmt.Sprintf("%s (0x%02X)", strings.Join(names, ", "), d)
}

func newCodeParseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "parse <code>",
		Short: "Parse a QR code or manual pairing code",
		Example: `  matter code parse "MT:Y3.13OTB00KA0648G00"
  matter code parse "34970112332"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := args[0]

			var payload *commissioning.SetupPayload
			var err error

			if len(code) > 3 && code[:3] == "MT:" {
				payload, err = commissioning.ParseQRCode(code)
			} else {
				payload, err = commissioning.ParseManualPairingCode(code)
				if err != nil {
					// Provide a hint if the code looks like it might be a QR code missing its prefix.
					if looksLikeQRPayload(code) {
						return fmt.Errorf("parsing setup code: %w\n\nHint: did you mean to include the \"MT:\" prefix? Try: matter code parse \"MT:%s\"", err, code)
					}
					return fmt.Errorf("parsing setup code: %w", err)
				}
			}
			if err != nil {
				return fmt.Errorf("parsing setup code: %w", err)
			}

			format, _ := cmd.Flags().GetString("format")
			f := output.New(format)

			if _, ok := f.(*output.TableFormatter); ok {
				w := cmd.OutOrStdout()
				qr, _ := payload.QRCode()
				manual, _ := payload.ManualPairingCode()

				fmt.Fprintf(w, "%s\n\n", output.Header("Setup Payload"))
				fmt.Fprintf(w, "  %s       %s\n", output.Label("Version:"), output.Value(fmt.Sprintf("%d", payload.Version)))
				fmt.Fprintf(w, "  %s     %s\n", output.Label("Vendor ID:"), output.Accent(vendordb.FormatVendorID(payload.VendorID)))
				fmt.Fprintf(w, "  %s    %s\n", output.Label("Product ID:"), output.Accent(fmt.Sprintf("0x%04X", payload.ProductID)))
				fmt.Fprintf(w, "  %s          %s\n", output.Label("Flow:"), output.Value(formatCommissioningFlow(payload.CommissioningFlow)))
				fmt.Fprintf(w, "  %s     %s\n", output.Label("Discovery:"), output.Value(formatDiscoveryCapabilities(payload.DiscoveryCapabilities)))
				fmt.Fprintf(w, "  %s %s %s\n", output.Label("Discriminator:"), output.Bold(fmt.Sprintf("%d", payload.Discriminator)), output.Muted(fmt.Sprintf("(0x%03X)", payload.Discriminator)))
				fmt.Fprintf(w, "  %s      %s\n", output.Label("Passcode:"), output.Bold(fmt.Sprintf("%d", payload.Passcode)))
				fmt.Fprintln(w)
				fmt.Fprintf(w, "  %s       %s\n", output.Label("QR Code:"), output.Success(qr))
				fmt.Fprintf(w, "  %s   %s\n", output.Label("Manual Code:"), output.Success(manual))
				return nil
			}

			return f.Format(cmd.OutOrStdout(), payload)
		},
	}
}

// looksLikeQRPayload reports whether s looks like it could be a base38-encoded
// QR payload that is missing the "MT:" prefix (all uppercase alphanumeric plus
// '-' and '.', and at least 8 characters long).
func looksLikeQRPayload(s string) bool {
	if len(s) < 8 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func newCodeGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a QR code and manual pairing code from parameters",
		Example: `  matter code generate --vid 0xFFF1 --pid 0x8000 --passcode 12345678 --discriminator 3840`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vid, _ := cmd.Flags().GetUint16("vid")
			pid, _ := cmd.Flags().GetUint16("pid")
			passcode, _ := cmd.Flags().GetUint32("passcode")
			disc, _ := cmd.Flags().GetUint16("discriminator")

			if passcode == 0 {
				return fmt.Errorf("--passcode is required")
			}

			payload := &commissioning.SetupPayload{
				VendorID:              vid,
				ProductID:             pid,
				Passcode:              passcode,
				Discriminator:         disc,
				DiscoveryCapabilities: commissioning.DiscoveryOnNetwork,
			}

			qr, err := payload.QRCode()
			if err != nil {
				return fmt.Errorf("generating QR code: %w", err)
			}

			manual, err := payload.ManualPairingCode()
			if err != nil {
				return fmt.Errorf("generating manual code: %w", err)
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "  %s  %s\n", output.Label("QR Code:"), output.Success(qr))
			fmt.Fprintf(w, "  %s  %s\n", output.Label("Manual Code:"), output.Success(manual))
			return nil
		},
	}
	cmd.Flags().Uint16("vid", 0, "vendor ID")
	cmd.Flags().Uint16("pid", 0, "product ID")
	cmd.Flags().Uint32("passcode", 0, "setup passcode (27-bit)")
	cmd.Flags().Uint16("discriminator", 3840, "discriminator (12-bit)")
	return cmd
}
