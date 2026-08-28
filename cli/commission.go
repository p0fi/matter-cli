// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/controller"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// makeAutoCommissioner is set by commission_ble.go (build tag !noble) to
// return a commissioner that tries BLE discovery before falling back to mDNS.
// When BLE support is compiled out this stays nil and we use mDNS-only.
var makeAutoCommissioner func(ctrl *controller.Controller) *commissioning.Commissioner

func init() {
	rootCmd.AddCommand(withGroup(newCommissionCmd(), groupDevices))
}

// newCommissionCmd creates the `matter-cli commission` subcommand group.
func newCommissionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commission",
		Short: "Commission Matter devices",
		// Commissioning needs exclusive write access to the database. Refuse
		// early if a session daemon is running so the user gets a clear message
		// instead of a cryptic lock-timeout error.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if daemon.NewClient("").IsRunning() {
				return fmt.Errorf(
					"a session daemon is running and holds the database lock\n" +
						"Stop it first with: matter session stop")
			}
			// Run the root PersistentPreRunE (logging setup, target resolution).
			return cmd.Root().PersistentPreRunE(cmd, args)
		},
	}
	cmd.AddCommand(newCommissionCodeCmd())
	cmd.AddCommand(newCommissionIPCmd())
	cmd.AddCommand(newCommissionOpenWindowCmd())
	cmd.AddCommand(newCommissionCloseWindowCmd())
	addBLECommissionCommands(cmd)
	return cmd
}

// stepDescription returns a user-friendly description for each commissioning step.
func stepDescription(step commissioning.CommissioningStep) string {
	descriptions := map[commissioning.CommissioningStep]string{
		commissioning.StepParseSetupCode:        "Parsing setup code",
		commissioning.StepDiscover:              "Discovering device",
		commissioning.StepEstablishPASE:         "Establishing PASE session",
		commissioning.StepArmFailsafe:           "Arming failsafe timer",
		commissioning.StepReadCommissioningInfo: "Reading commissioning info",
		commissioning.StepReadBasicInfo:         "Reading basic information",
		commissioning.StepAttestationRequest:    "Requesting attestation",
		commissioning.StepValidateAttestation:   "Validating attestation",
		commissioning.StepCSRRequest:            "Requesting CSR",
		commissioning.StepGenerateNOC:           "Generating NOC",
		commissioning.StepAddTrustedRoot:        "Adding trusted root certificate",
		commissioning.StepAddNOC:                "Adding NOC",
		commissioning.StepNetworkSetup:          "Setting up network",
		commissioning.StepNetworkConnect:        "Connecting to network",
		commissioning.StepCommissioningComplete: "Completing commissioning",
		commissioning.StepEstablishCASE:         "Establishing CASE session",
	}
	if desc, ok := descriptions[step]; ok {
		return desc
	}
	return step.String()
}

// getStringFlagOrViper returns the flag value when the flag was explicitly set,
// otherwise falls back to the viper key (config file or env var).
func getStringFlagOrViper(cmd *cobra.Command, flagName, viperKey string) string {
	if cmd.Flags().Changed(flagName) {
		v, _ := cmd.Flags().GetString(flagName)
		return v
	}
	return viper.GetString(viperKey)
}

// buildNetworkCreds builds network credentials from CLI flags if provided,
// falling back to config file values (wifi.ssid, wifi.password, thread.dataset)
// and environment variables (MATTER_WIFI_SSID, MATTER_WIFI_PASSWORD,
// MATTER_THREAD_DATASET) when flags are not explicitly set.
// Returns the credentials and an error if validation fails.
func buildNetworkCreds(cmd *cobra.Command) (*commissioning.NetworkCredentials, error) {
	ssid := getStringFlagOrViper(cmd, "wifi-ssid", "wifi.ssid")
	password := getStringFlagOrViper(cmd, "wifi-password", "wifi.password")
	threadHex := getStringFlagOrViper(cmd, "thread-dataset", "thread.dataset")

	if ssid != "" {
		creds := commissioning.NewWiFiCredentials(ssid, password)
		return &creds, nil
	}
	if threadHex != "" {
		dataset, err := hex.DecodeString(strings.TrimPrefix(threadHex, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid --thread-dataset: not valid hex: %w", err)
		}
		if err := commissioning.ValidateThreadDataset(dataset); err != nil {
			return nil, fmt.Errorf("invalid --thread-dataset: %w", err)
		}
		creds := commissioning.NewThreadCredentials(dataset)
		return &creds, nil
	}
	return nil, nil
}

func newCommissionCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code <setup-code>",
		Short: "Commission a device using a QR or manual pairing code",
		Example: `  matter commission code "MT:Y3.13OTB00KA0648G00"
  matter @5 commission code "MT:Y3.13OTB00KA0648G00"`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Node ID is optional for commissioning — if a @target was given
			// (e.g. matter @5 commission code "...") use its node ID, otherwise
			// auto-assign the next available ID.
			var nodeID uint64
			if resolvedTarget != nil {
				nodeID = resolvedTarget.NodeID
			}
			if nodeID == 0 {
				var err error
				nodeID, err = nextNodeID()
				if err != nil {
					return err
				}
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

			verbose, _ := cmd.Flags().GetBool("verbose")
			stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

			// Use BLE+mDNS auto-detection when BLE support is compiled in,
			// otherwise fall back to mDNS-only discovery.
			var commissioner *commissioning.Commissioner
			if makeAutoCommissioner != nil {
				commissioner = makeAutoCommissioner(ctrl)
			} else {
				commissioner = ctrl.NewCommissioner()
			}
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

			stepper.Step(fmt.Sprintf("Commissioning device with code %s as node %s",
				output.Accent(args[0]), output.Bold(fmt.Sprintf("%d", nodeID))))

			ctx := cmd.Context()
			result, err := commissioner.Commission(ctx, params)
			if err != nil {
				return fmt.Errorf("Commissioning failed: %w", err)
			}

			node := buildNodeFromResult(nodeID, result)
			if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to save node: %v\n",
					output.WarningIcon(), saveErr)
			}

			stepper.Success(fmt.Sprintf("Device commissioned successfully as node %s",
				output.Bold(fmt.Sprintf("%d", nodeID))))
			printDiscoverTip(cmd, nodeID)
			return nil
		},
	}
	cmd.Flags().String("wifi-ssid", "", "WiFi SSID for network provisioning")
	cmd.Flags().String("wifi-password", "", "WiFi password for network provisioning")
	cmd.Flags().String("thread-dataset", "", "hex-encoded Thread operational dataset")
	return cmd
}

func newCommissionIPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ip <address>",
		Short: "Commission a device at a known IP address",
		Example: `  matter commission ip 192.168.1.100 --setup-pin 12345678
  matter @5 commission ip 192.168.1.100 --setup-pin 12345678`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var nodeID uint64
			if resolvedTarget != nil {
				nodeID = resolvedTarget.NodeID
			}
			if nodeID == 0 {
				var err error
				nodeID, err = nextNodeID()
				if err != nil {
					return err
				}
			}
			pin, _ := cmd.Flags().GetUint32("setup-pin")
			if pin == 0 {
				return fmt.Errorf("--setup-pin is required")
			}

			addr := args[0]
			// Add default Matter port if not specified.
			if !strings.Contains(addr, ":") {
				addr = addr + ":5540"
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

			verbose, _ := cmd.Flags().GetBool("verbose")
			stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

			commissioner := ctrl.NewCommissioner()
			// Override the discoverer with a static one that returns the given address.
			commissioner.Discoverer = &controller.StaticDiscoverer{Addr: addr}
			commissioner.OnProgress = func(step commissioning.CommissioningStep) {
				stepper.Step(stepDescription(step))
			}

			network, err := buildNetworkCreds(cmd)
			if err != nil {
				stepper.Fail(err.Error())
				return err
			}

			params := commissioning.CommissioningParams{
				Passcode:  pin,
				NodeID:    nodeID,
				Network:   network,
				OnNetwork: true,
			}

			stepper.Step(fmt.Sprintf("Commissioning device at %s with pin %s as node %s",
				output.Accent(args[0]), output.Bold(fmt.Sprintf("%d", pin)),
				output.Bold(fmt.Sprintf("%d", nodeID))))

			ctx := cmd.Context()
			result, err := commissioner.Commission(ctx, params)
			if err != nil {
				return fmt.Errorf("Commissioning failed: %w", err)
			}

			node := buildNodeFromResult(nodeID, result)
			if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to save node: %v\n",
					output.WarningIcon(), saveErr)
			}

			stepper.Success(fmt.Sprintf("Device commissioned successfully as node %s",
				output.Bold(fmt.Sprintf("%d", nodeID))))
			printDiscoverTip(cmd, nodeID)
			return nil
		},
	}
	cmd.Flags().Uint32("setup-pin", 0, "setup PIN/passcode")
	cmd.Flags().String("wifi-ssid", "", "WiFi SSID for network provisioning")
	cmd.Flags().String("wifi-password", "", "WiFi password for network provisioning")
	cmd.Flags().String("thread-dataset", "", "hex-encoded Thread operational dataset")
	return cmd
}

// printDiscoverTip points the user at `cluster discover` after a successful
// commission. Reading every cluster's AttributeList during commissioning itself
// would add latency and another failure surface to the most fragile part of the
// flow, so the cache stays cold until the user asks for it.
//
// The suggestion is rendered as a tip rather than as another stepper line: it
// reports nothing about the commission that just ran, and the user is free to
// ignore it.
func printDiscoverTip(cmd *cobra.Command, nodeID uint64) {
	command := output.Command(fmt.Sprintf("matter @%d cluster discover", nodeID))
	fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", output.Tip(
		output.Muted("Run ")+command+
			output.Muted(" to enable attribute-name completion for this device")))
}

// nextNodeID returns the next available node ID by scanning existing nodes.
func nextNodeID() (uint64, error) {
	s, err := openStore()
	if err != nil {
		return 1, nil // no store yet, start at 1
	}
	defer s.Close()

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}
	nodes, err := s.ListNodes(fabricID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 1, nil
		}
		return 0, fmt.Errorf("listing nodes: %w", err)
	}

	var maxID uint64
	for _, n := range nodes {
		if n.ID > maxID {
			maxID = n.ID
		}
	}
	return maxID + 1, nil
}

// buildNodeFromResult creates a store.Node populated from the commissioning result.
func buildNodeFromResult(nodeID uint64, result *commissioning.CommissioningResult) *store.Node {
	node := &store.Node{
		ID:                   nodeID,
		VendorID:             result.VendorID,
		ProductID:            result.ProductID,
		SpecificationVersion: result.SpecificationVersion,
		SoftwareVersion:      result.SoftwareVersion,
		SerialNumber:         result.SerialNumber,
		LastAddress:          result.Address,
		LastSeen:             time.Now(),
	}
	if result.ProductName != "" {
		node.Name = result.ProductName
	} else if result.VendorName != "" {
		node.Name = result.VendorName
	}
	for _, ep := range result.Endpoints {
		storeEp := store.Endpoint{ID: ep.ID}
		for _, dt := range ep.DeviceTypes {
			storeEp.DeviceTypes = append(storeEp.DeviceTypes, store.DeviceType{
				ID:       dt.ID,
				Revision: dt.Revision,
			})
		}
		for _, cid := range ep.ServerClusters {
			ref := store.ClusterRef{ID: cid, Side: "server"}
			if cl, ok := clusters.Global.ClusterByID(cid); ok {
				ref.Name = cl.DisplayName
			}
			storeEp.Clusters = append(storeEp.Clusters, ref)
		}
		node.Endpoints = append(node.Endpoints, storeEp)
	}
	return node
}
