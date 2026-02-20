// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/commissioning"
	"github.com/p0fi/matter-cli/internal/controller"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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
	cmd.AddCommand(newCommissionForgetCmd())
	return cmd
}

// stepDescription returns a user-friendly description for each commissioning step.
func stepDescription(step commissioning.CommissioningStep) string {
	descriptions := map[commissioning.CommissioningStep]string{
		commissioning.StepParseSetupCode:        "Parsing setup code",
		commissioning.StepDiscover:              "Discovering device",
		commissioning.StepEstablishPASE:         "Establishing PASE session",
		commissioning.StepArmFailsafe:           "Arming failsafe timer",
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

// buildNetworkCreds builds network credentials from CLI flags if provided.
func buildNetworkCreds(cmd *cobra.Command) *commissioning.NetworkCredentials {
	ssid, _ := cmd.Flags().GetString("wifi-ssid")
	password, _ := cmd.Flags().GetString("wifi-password")
	threadHex, _ := cmd.Flags().GetString("thread-dataset")

	if ssid != "" {
		creds := commissioning.NewWiFiCredentials(ssid, password)
		return &creds
	}
	if threadHex != "" {
		dataset, err := hex.DecodeString(strings.TrimPrefix(threadHex, "0x"))
		if err == nil {
			creds := commissioning.NewThreadCredentials(dataset)
			return &creds
		}
	}
	return nil
}

func newCommissionCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code <setup-code>",
		Short: "Commission a device using a QR or manual pairing code",
		Example: `  matter commission code "MT:Y3.13OTB00KA0648G00"
  matter commission code "34970112332" --node 5
  matter @5 commission code "MT:Y3.13OTB00KA0648G00"`,
		Args: cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _ := cmd.Flags().GetUint64("node")
			if nodeID == 0 {
				// For commission, node ID is optional — auto-assign if not given.
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

			commissioner := ctrl.NewCommissioner()
			commissioner.OnProgress = func(step commissioning.CommissioningStep) {
				stepper.Step(stepDescription(step))
			}

			params := commissioning.CommissioningParams{
				SetupCode: args[0],
				NodeID:    nodeID,
				Network:   buildNetworkCreds(cmd),
			}

			stepper.Step(fmt.Sprintf("Commissioning device with code %s as node %s",
				output.Accent(args[0]), output.Bold(fmt.Sprintf("%d", nodeID))))

			ctx := cmd.Context()
			result, err := commissioner.Commission(ctx, params)
			if err != nil {
				stepper.Fail(fmt.Sprintf("Commissioning failed: %v", err))
				return fmt.Errorf("commissioning failed: %w", err)
			}

			node := buildNodeFromResult(nodeID, result)
			if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to save node: %v\n",
					output.WarningIcon(), saveErr)
			}

			stepper.Success(fmt.Sprintf("Device commissioned successfully as node %s",
				output.Bold(fmt.Sprintf("%d", nodeID))))
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
  matter @5 commission ip 192.168.1.100 --setup-pin 12345678
  matter commission ip 192.168.1.100 --setup-pin 12345678 --node 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _ := cmd.Flags().GetUint64("node")
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

			params := commissioning.CommissioningParams{
				Passcode: pin,
				NodeID:   nodeID,
				Network:  buildNetworkCreds(cmd),
			}

			stepper.Step(fmt.Sprintf("Commissioning device at %s with pin %s as node %s",
				output.Accent(args[0]), output.Bold(fmt.Sprintf("%d", pin)),
				output.Bold(fmt.Sprintf("%d", nodeID))))

			ctx := cmd.Context()
			result, err := commissioner.Commission(ctx, params)
			if err != nil {
				stepper.Fail(fmt.Sprintf("Commissioning failed: %v", err))
				return fmt.Errorf("commissioning failed: %w", err)
			}

			node := buildNodeFromResult(nodeID, result)
			if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to save node: %v\n",
					output.WarningIcon(), saveErr)
			}

			stepper.Success(fmt.Sprintf("Device commissioned successfully as node %s",
				output.Bold(fmt.Sprintf("%d", nodeID))))
			return nil
		},
	}
	cmd.Flags().Uint32("setup-pin", 0, "setup PIN/passcode")
	cmd.Flags().String("wifi-ssid", "", "WiFi SSID for network provisioning")
	cmd.Flags().String("wifi-password", "", "WiFi password for network provisioning")
	cmd.Flags().String("thread-dataset", "", "hex-encoded Thread operational dataset")
	return cmd
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
		ID:          nodeID,
		VendorID:    result.VendorID,
		ProductID:   result.ProductID,
		LastAddress: result.Address,
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
			} else {
				ref.Name = fmt.Sprintf("0x%04X", cid)
			}
			storeEp.Clusters = append(storeEp.Clusters, ref)
		}
		node.Endpoints = append(node.Endpoints, storeEp)
	}
	return node
}

func newCommissionForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget",
		Short: "Remove a commissioned device from local storage",
		Example: `  matter @1 commission forget
  matter commission forget --node 1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _, err := requireTarget(cmd)
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
			if err := s.DeleteNode(fabricID, nodeID); err != nil {
				return fmt.Errorf("removing node %d: %w", nodeID, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s Node %s removed.\n",
				output.SuccessIcon(), output.Bold(fmt.Sprintf("%d", nodeID)))
			return nil
		},
	}
}
