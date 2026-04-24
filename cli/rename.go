// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/p0fi/matter-cli/cli/completion"
	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters/basicinformation"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/tlv"
	"github.com/spf13/cobra"
)

// nodeLabelMaxBytes is the maximum byte length of BasicInformation.NodeLabel
// per the Matter spec (string max 32 bytes, UTF-8 encoded).
const nodeLabelMaxBytes = 32

func init() {
	rootCmd.AddCommand(withGroup(newRenameCmd(), groupDevices))
	// Rename operates on a node as a whole, not an endpoint — hide it from
	// completion whenever the user has an endpoint-explicit target set.
	deviceOnlyCommands["rename"] = true
}

// newRenameCmd creates the top-level `matter rename` command which sets a
// friendly name for a commissioned device both locally (used for CLI listings
// and completion) and on the device (BasicInformation.NodeLabel, so other
// controllers see the same label).
func newRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename [@target] <name>",
		Short: "Rename a commissioned device locally and on the device",
		Long: `Set a friendly name for a commissioned device.

The local store name is updated (used for CLI listings and completion) and
NodeLabel is written to the device's BasicInformation cluster so other
controllers see the same label.

When the device is unreachable the local name is still updated and a warning
is printed. Use --local to skip the device write up front.

The --reset flag re-reads ProductName from the device and uses it as the new
name, also clearing NodeLabel on the device.`,
		Example: `  matter rename @1 "Kitchen Light"
  matter @1 rename "Living Room"
  matter rename @1 --reset
  matter rename @1 "Porch Lamp" --local`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completion.TargetCompletionFunc(nil),
		RunE:              runRename,
	}
	cmd.Flags().Bool("reset", false, "reset name by re-reading ProductName from the device")
	cmd.Flags().Bool("local", false, "only update the local store; skip the NodeLabel write")
	return cmd
}

// runRename implements `matter rename`.
func runRename(cmd *cobra.Command, args []string) error {
	newName, err := parseRenameArgs(args)
	if err != nil {
		return err
	}

	nodeID, _, err := requireTarget(cmd)
	if err != nil {
		return err
	}

	reset, _ := cmd.Flags().GetBool("reset")
	local, _ := cmd.Flags().GetBool("local")

	if reset && newName != "" {
		return errors.New("--reset cannot be combined with a positional name")
	}
	if !reset {
		if err := validateNodeLabel(newName); err != nil {
			return err
		}
	}

	fid := fabricID()
	node, err := loadNodeForCompletion(fid, nodeID)
	if err != nil {
		return fmt.Errorf("looking up node %d: %w", nodeID, err)
	}
	oldName := node.Name

	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	ctx := cmd.Context()

	if reset {
		stepper.Step("Reading ProductName from device")
		productName, err := readProductName(ctx, nodeID)
		if err != nil {
			stepper.Fail(fmt.Sprintf("Reading ProductName failed: %v", err))
			return fmt.Errorf("reading ProductName: %w", err)
		}
		if strings.TrimSpace(productName) == "" {
			return errors.New("device returned empty ProductName — specify an explicit name instead")
		}
		newName = productName

		if !local {
			stepper.Step("Clearing NodeLabel on device")
			if err := writeNodeLabel(ctx, nodeID, ""); err != nil {
				stepper.Fail(fmt.Sprintf("Clearing NodeLabel failed: %v", err))
				fmt.Fprintf(cmd.ErrOrStderr(),
					"%s Device did not accept NodeLabel clear; local name is still being updated.\n",
					output.WarningIcon())
			}
		}
	} else if !local {
		stepper.Step(fmt.Sprintf("Writing NodeLabel=%q to device", newName))
		if err := writeNodeLabel(ctx, nodeID, newName); err != nil {
			stepper.Fail(fmt.Sprintf("Writing NodeLabel failed: %v", err))
			fmt.Fprintf(cmd.ErrOrStderr(),
				"%s Device is unreachable; local name is still being updated. Re-run with %s when it's back online.\n",
				output.WarningIcon(), output.Bold("matter rename"))
		}
	}

	warnOnNameConflict(cmd, fid, nodeID, newName)

	node.Name = newName
	if err := persistNode(fid, node); err != nil {
		return fmt.Errorf("saving node: %w", err)
	}

	stepper.Success(fmt.Sprintf("Renamed node %d: %s → %s",
		nodeID,
		output.Bold(displayOldName(oldName)),
		output.Bold(newName)))
	return nil
}

// parseRenameArgs accepts the positional forms:
//
//	rename @1 "Kitchen"   (len=2, first arg is target)
//	@1 rename "Kitchen"   (len=1, target already resolved inline)
//	rename @1             (len=1, target only — for --reset)
//	@1 rename             (len=0, target only — for --reset)
//
// It returns the user-supplied name (empty when only a target was given). If
// the first argument is a @target it is parsed and stored in resolvedTarget.
func parseRenameArgs(args []string) (string, error) {
	idx := 0
	if len(args) > 0 && IsTargetArg(args[0]) {
		t, err := ParseTarget(args[0])
		if err != nil {
			return "", fmt.Errorf("invalid target %q: %w", args[0], err)
		}
		resolvedTarget = t
		idx = 1
	}
	if idx < len(args) {
		return strings.Join(args[idx:], " "), nil
	}
	return "", nil
}

// validateNodeLabel enforces the Matter NodeLabel constraints plus basic
// ergonomics (no empty/whitespace-only strings).
func validateNodeLabel(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New(`name is required (use "matter rename @<id> --reset" to restore the original ProductName)`)
	}
	if name != trimmed {
		return errors.New("name must not start or end with whitespace")
	}
	if len(name) > nodeLabelMaxBytes {
		return fmt.Errorf("name exceeds the %d-byte NodeLabel limit (got %d bytes)", nodeLabelMaxBytes, len(name))
	}
	return nil
}

// warnOnNameConflict prints a warning (but does not block) when another node
// in the same fabric already has the same name.
func warnOnNameConflict(cmd *cobra.Command, fabricID, nodeID uint64, name string) {
	nodes, err := loadNodesForCompletion(fabricID)
	if err != nil {
		return
	}
	for _, n := range nodes {
		if n.ID == nodeID {
			continue
		}
		if strings.EqualFold(n.Name, name) {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"%s Name %q is also used by node %d — completion will prefer the lowest node ID.\n",
				output.WarningIcon(), name, n.ID)
			return
		}
	}
}

// displayOldName returns a human-friendly representation of a prior name for
// rename confirmation output.
func displayOldName(old string) string {
	if old == "" {
		return "(unnamed)"
	}
	return old
}

// writeNodeLabel writes BasicInformation.NodeLabel (endpoint 0) on the device,
// preferring the session daemon when available so a cached CASE session is
// reused.
func writeNodeLabel(ctx context.Context, nodeID uint64, label string) error {
	const ep0 = uint16(0)

	w := tlv.NewWriter()
	if err := w.PutUTF8String(tlv.AnonymousTag(), label); err != nil {
		return fmt.Errorf("encoding NodeLabel: %w", err)
	}
	payload := w.Bytes()

	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("rename: writing NodeLabel via daemon", "node", nodeID)
		resp, err := dc.Write(daemon.AttrWriteReq{
			Endpoint:    ep0,
			ClusterID:   basicinformation.ID,
			AttributeID: basicinformation.AttrNodeLabel,
			Data:        daemon.EncodeFields(payload),
		})
		if err != nil {
			return fmt.Errorf("writing NodeLabel: %w", err)
		}
		for _, st := range resp.Statuses {
			if st.StatusCode != 0 {
				return fmt.Errorf("NodeLabel write rejected: status 0x%02X", st.StatusCode)
			}
		}
		return nil
	}

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("connecting to node: %w", err)
	}
	defer cleanup()

	statuses, err := client.Write(ctx, session, interaction.AttributeWrite{
		Path: interaction.NewAttributePath(ep0, basicinformation.ID, basicinformation.AttrNodeLabel),
		Data: payload,
	})
	if err != nil {
		return fmt.Errorf("writing NodeLabel: %w", err)
	}
	for _, st := range statuses {
		if st.Status.Status != 0 {
			return fmt.Errorf("NodeLabel write rejected: status 0x%02X", st.Status.Status)
		}
	}
	return nil
}

// readProductName reads BasicInformation.ProductName (endpoint 0) from the
// device. Used by --reset to restore the factory label.
func readProductName(ctx context.Context, nodeID uint64) (string, error) {
	const ep0 = uint16(0)

	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("rename: reading ProductName via daemon", "node", nodeID)
		resp, err := dc.Read(daemon.AttrPathReq{
			Endpoint:    ep0,
			ClusterID:   basicinformation.ID,
			AttributeID: basicinformation.AttrProductName,
		})
		if err != nil {
			return "", err
		}
		if len(resp.Reports) == 0 {
			return "", errors.New("no report data")
		}
		r := resp.Reports[0]
		if r.StatusCode != 0 {
			return "", fmt.Errorf("status 0x%02X", r.StatusCode)
		}
		data, derr := daemon.DecodeFields(r.Data)
		if derr != nil {
			return "", fmt.Errorf("decoding ProductName: %w", derr)
		}
		return decodeTLVString(data)
	}

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		return "", err
	}
	defer cleanup()

	reports, err := client.Read(ctx, session,
		interaction.NewAttributePath(ep0, basicinformation.ID, basicinformation.AttrProductName))
	if err != nil {
		return "", err
	}
	for _, r := range reports {
		if r.Status != nil {
			return "", fmt.Errorf("status 0x%02X", r.Status.Status.Status)
		}
		if r.Data != nil {
			return decodeTLVString(r.Data.Data)
		}
	}
	return "", errors.New("no report data")
}

// decodeTLVString extracts a single UTF-8 string from a raw TLV element.
func decodeTLVString(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("empty TLV")
	}
	r := tlv.NewReader(bytes.NewReader(raw))
	if err := r.Next(); err != nil {
		return "", err
	}
	v, ok := r.Value().(string)
	if !ok {
		return "", fmt.Errorf("expected UTF-8 string TLV (got %T)", r.Value())
	}
	return v, nil
}
