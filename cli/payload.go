// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(withGroup(newPayloadCmd(), groupTools))
}

// newPayloadCmd creates the `matter payload` subcommand group.
func newPayloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payload",
		Short: "Parse or generate Matter setup payloads (QR codes, manual pairing codes)",
	}
	cmd.AddCommand(newPayloadParseCmd())
	cmd.AddCommand(newPayloadGenerateCmd())
	return cmd
}

func newPayloadParseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "parse <code>",
		Short: "Parse a QR code or manual pairing code",
		Example: `  matter payload parse "MT:Y3.13OTB00KA0648G00"
  matter payload parse "34970112332"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code := args[0]

			var payload *commissioning.SetupPayload
			var err error

			if len(code) > 3 && code[:3] == "MT:" {
				payload, err = commissioning.ParseQRCode(code)
			} else {
				payload, err = commissioning.ParseManualPairingCode(code)
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
				fmt.Fprintf(w, "  %s     %s\n", output.Label("Vendor ID:"), output.Accent(fmt.Sprintf("0x%04X", payload.VendorID)))
				fmt.Fprintf(w, "  %s    %s\n", output.Label("Product ID:"), output.Accent(fmt.Sprintf("0x%04X", payload.ProductID)))
				fmt.Fprintf(w, "  %s          %s\n", output.Label("Flow:"), output.Value(fmt.Sprintf("%d", payload.CommissioningFlow)))
				fmt.Fprintf(w, "  %s     %s\n", output.Label("Discovery:"), output.Value(fmt.Sprintf("0x%02X", payload.DiscoveryCapabilities)))
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

func newPayloadGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a QR code and manual pairing code from parameters",
		Example: `  matter payload generate --vid 0xFFF1 --pid 0x8000 --passcode 12345678 --discriminator 3840`,
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
