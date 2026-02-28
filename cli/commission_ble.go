// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

//go:build !noble

package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/controller"
	"github.com/p0fi/matter-cli/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	// Enable BLE auto-detection for `matter commission code`. When this
	// build includes BLE support, the generic commission-by-code command
	// will try BLE discovery first (matching manual-pairing-code short
	// discriminators) before falling back to mDNS.
	makeAutoCommissioner = func(ctrl *controller.Controller) *commissioning.Commissioner {
		adapter := transport.NewDefaultBLEAdapter()
		return ctrl.NewCommissionerWithTransport(controller.TransportAuto, adapter)
	}
}

// addBLECommissionCommands adds BLE-specific subcommands to the commission
// parent command:
//   - matter commission ble <setup-code>       — scan + commission via QR/manual code
//   - matter commission ble-address <addr>     — commission a known BLE address directly
func addBLECommissionCommands(parent *cobra.Command) {
	parent.AddCommand(newCommissionBLECmd())
	parent.AddCommand(newCommissionBLEAddressCmd())
}

// newCommissionBLECmd creates `matter commission ble <setup-code>`.
//
// The setup code (QR "MT:…" or 11-digit manual code) is parsed to extract the
// 12-bit discriminator and passcode. The system BLE adapter scans for a device
// advertising the Matter service UUID (0xFFF6) whose service data encodes that
// discriminator. PASE is then run over the BLE/BTP transport; afterwards CASE
// is established over IP for ongoing communication.
func newCommissionBLECmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ble <setup-code>",
		Short: "Commission a device over BLE using a QR or manual pairing code",
		Long: `Commission a Matter device over Bluetooth Low Energy.

The setup code (QR code "MT:..." or manual pairing code) is parsed to extract
the device discriminator and passcode. The local BLE adapter scans for a device
advertising the Matter BLE service UUID (0xFFF6) with matching discriminator.
Once found, PASE session establishment runs over the BLE/BTP transport. After
the device receives its operational credentials, a CASE session is established
over IP for ongoing operation.

Requires Bluetooth hardware and appropriate OS permissions:
  macOS  – Bluetooth permission must be granted to the terminal app.
  Linux  – User needs cap_net_admin or membership in the "bluetooth" group.`,
		Example: `  matter commission ble "MT:Y3.13OTB00KA0648G00"
  matter commission ble "34970112332"
  matter @5 commission ble "MT:Y3.13OTB00KA0648G00" --wifi-ssid MyNet --wifi-password secret`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: runCommissionBLE,
	}
	cmd.Flags().Duration("scan-timeout", 30*time.Second, "timeout for the BLE device scan")
	cmd.Flags().String("wifi-ssid", "", "WiFi SSID for network provisioning")
	cmd.Flags().String("wifi-password", "", "WiFi password for network provisioning")
	cmd.Flags().String("thread-dataset", "", "hex-encoded Thread operational dataset")
	return cmd
}

func runCommissionBLE(cmd *cobra.Command, args []string) error {
	nodeID, err := resolveOrAllocNodeID()
	if err != nil {
		return err
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	ctrl, err := controller.New(controller.Config{
		Store:    s,
		FabricID: fabricID,
	})
	if err != nil {
		return fmt.Errorf("creating controller: %w", err)
	}
	defer ctrl.Close()

	adapter := transport.NewDefaultBLEAdapter()

	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	commissioner := ctrl.NewCommissionerWithTransport(controller.TransportBLE, adapter)
	commissioner.OnProgress = func(step commissioning.CommissioningStep) {
		stepper.Step(stepDescription(step))
	}

	network, err := buildNetworkCreds(cmd)
	if err != nil {
		stepper.Fail(err.Error())
		return err
	}

	params := commissioning.CommissioningParams{
		SetupCode: args[0],
		NodeID:    nodeID,
		Network:   network,
	}

	stepper.Step(fmt.Sprintf("Commissioning device over BLE with code %s as node %s",
		output.Accent(args[0]), output.Bold(fmt.Sprintf("%d", nodeID))))

	ctx := cmd.Context()
	result, err := commissioner.Commission(ctx, params)
	if err != nil {
		stepper.Fail(fmt.Sprintf("BLE commissioning failed: %v", err))
		return fmt.Errorf("BLE commissioning failed: %w", err)
	}

	node := buildNodeFromResult(nodeID, result)
	if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to save node: %v\n",
			output.WarningIcon(), saveErr)
	}

	stepper.Success(fmt.Sprintf("Device commissioned successfully over BLE as node %s",
		output.Bold(fmt.Sprintf("%d", nodeID))))
	return nil
}

// newCommissionBLEAddressCmd creates `matter commission ble-address <ble-address>`.
//
// Use this when you already know the BLE address of the device (e.g. from a
// prior `matter discover ble` scan) and want to skip the BLE scan step.
// On Linux the address is a MAC ("AA:BB:CC:DD:EE:FF"); on macOS it is the
// CoreBluetooth UUID assigned to the peripheral.
func newCommissionBLEAddressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ble-address <ble-address>",
		Short: "Commission a device at a known BLE address",
		Long: `Commission a Matter device whose BLE address is already known.

On Linux the BLE address is the Bluetooth MAC address printed in colon-hex
notation (AA:BB:CC:DD:EE:FF). On macOS it is the CoreBluetooth-assigned UUID
string (12345678-1234-1234-1234-123456789ABC) because CoreBluetooth does not
expose hardware MAC addresses.

Use 'matter discover ble' to find device addresses before commissioning.`,
		Example: `  matter commission ble-address "AA:BB:CC:DD:EE:FF" --setup-pin 20202021
  matter commission ble-address "12345678-1234-1234-1234-123456789ABC" --setup-pin 20202021
  matter @3 commission ble-address "AA:BB:CC:DD:EE:FF" --setup-pin 20202021 --wifi-ssid Home`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: runCommissionBLEAddress,
	}
	cmd.Flags().Uint32("setup-pin", 0, "device setup PIN / passcode (required)")
	cmd.Flags().String("wifi-ssid", "", "WiFi SSID for network provisioning")
	cmd.Flags().String("wifi-password", "", "WiFi password for network provisioning")
	cmd.Flags().String("thread-dataset", "", "hex-encoded Thread operational dataset")
	return cmd
}

func runCommissionBLEAddress(cmd *cobra.Command, args []string) error {
	pin, _ := cmd.Flags().GetUint32("setup-pin")
	if pin == 0 {
		return fmt.Errorf("--setup-pin is required")
	}

	nodeID, err := resolveOrAllocNodeID()
	if err != nil {
		return err
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	ctrl, err := controller.New(controller.Config{
		Store:    s,
		FabricID: fabricID,
	})
	if err != nil {
		return fmt.Errorf("creating controller: %w", err)
	}
	defer ctrl.Close()

	adapter := transport.NewDefaultBLEAdapter()
	bleAddr := transport.BLEAddress(args[0])

	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	// Build a BLE commissioner but replace the default BLEBrowser discoverer
	// with a static one that immediately returns the known address. This skips
	// the BLE scan step while keeping all other commissioning logic intact.
	commissioner := ctrl.NewCommissionerWithTransport(controller.TransportBLE, adapter)
	commissioner.Discoverer = &staticBLEDiscoverer{addr: "ble://" + string(bleAddr)}
	commissioner.OnProgress = func(step commissioning.CommissioningStep) {
		stepper.Step(stepDescription(step))
	}

	network, err := buildNetworkCreds(cmd)
	if err != nil {
		stepper.Fail(err.Error())
		return err
	}

	params := commissioning.CommissioningParams{
		Passcode: pin,
		NodeID:   nodeID,
		Network:  network,
	}

	stepper.Step(fmt.Sprintf("Commissioning device at BLE address %s with PIN %s as node %s",
		output.Accent(args[0]), output.Bold(fmt.Sprintf("%d", pin)),
		output.Bold(fmt.Sprintf("%d", nodeID))))

	ctx := cmd.Context()
	result, err := commissioner.Commission(ctx, params)
	if err != nil {
		stepper.Fail(fmt.Sprintf("BLE commissioning failed: %v", err))
		return fmt.Errorf("BLE commissioning failed: %w", err)
	}

	node := buildNodeFromResult(nodeID, result)
	if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to save node: %v\n",
			output.WarningIcon(), saveErr)
	}

	stepper.Success(fmt.Sprintf("Device commissioned successfully over BLE as node %s",
		output.Bold(fmt.Sprintf("%d", nodeID))))
	return nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// resolveOrAllocNodeID returns the node ID from the resolved CLI target (@N),
// or allocates the next available ID if no target was specified.
func resolveOrAllocNodeID() (uint64, error) {
	if resolvedTarget != nil && resolvedTarget.NodeID != 0 {
		return resolvedTarget.NodeID, nil
	}
	return nextNodeID()
}

// staticBLEDiscoverer implements commissioning.DeviceDiscoverer by returning a
// pre-configured BLE address regardless of the discriminator value. It is used
// by `commission ble-address` to bypass the BLE scan step when the caller
// already knows the device's address.
type staticBLEDiscoverer struct {
	addr string // "ble://<platform-address>"
}

// DiscoverCommissionable returns the pre-configured BLE address unconditionally.
func (d *staticBLEDiscoverer) DiscoverCommissionable(_ context.Context, _ uint16) (string, error) {
	return d.addr, nil
}
