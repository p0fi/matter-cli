// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/p0fi/matter-cli/cli/completion"
	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters/operationalcredentials"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/tlv"
	"github.com/spf13/cobra"
)

// NodeOperationalCertStatusEnum values from the OperationalCredentials cluster
// (Matter spec §11.18.5.3). Only the ones we surface to the user are listed.
const (
	nocStatusOK                 uint8 = 0x00
	nocStatusInvalidFabricIndex uint8 = 0x0B
)

func init() {
	rootCmd.AddCommand(withGroup(newDecommissionCmd(), groupDevices))
	// Decommission operates on a node as a whole, not an endpoint — hide it
	// from completion whenever the user has an endpoint-explicit target set.
	deviceOnlyCommands["decommission"] = true
}

// newDecommissionCmd creates the top-level `matter decommission` command which
// is the proper counterpart to `matter commission`: it sends RemoveFabric to
// the device over CASE so the device drops our fabric credentials, then
// deletes the node from the local store. `matter fabric remove` is the
// local-only equivalent (device is not notified).
func newDecommissionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decommission [@target]",
		Short: "Remove a commissioned device from our fabric (notifies the device)",
		Long: `Remove a commissioned device from our fabric, both on the device and in
the local store.

The device is sent the RemoveFabric command on the OperationalCredentials
cluster. If this was the device's last fabric, it automatically re-enters
commissioning mode (no factory reset needed). The node is then deleted from
the local store.

For a local-only delete (e.g. when the device is permanently offline), use
"matter fabric remove" instead.`,
		Example: `  matter decommission @1
  matter @1 decommission
  matter decommission @1 --force   # delete locally even if device is unreachable`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completion.TargetCompletionFunc(),
		RunE:              runDecommission,
	}
	// Note: -f is reserved globally for --format, so --force has no short alias.
	cmd.Flags().Bool("force", false, "delete from local store even if the device is unreachable")
	return cmd
}

// runDecommission implements `matter decommission`.
func runDecommission(cmd *cobra.Command, args []string) error {
	// Accept `matter decommission @1` (positional) and `matter @1 decommission`
	// (inline, already resolved by PersistentPreRunE).
	if len(args) == 1 {
		raw := args[0]
		if !IsTargetArg(raw) {
			raw = "@" + raw
		}
		t, err := ParseTarget(raw)
		if err != nil {
			return fmt.Errorf("invalid target %q: %w", args[0], err)
		}
		resolvedTarget = t
	}

	nodeID, _, err := requireTarget(cmd)
	if err != nil {
		return err
	}
	fid := fabricID()
	force, _ := cmd.Flags().GetBool("force")

	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	// Capture the friendly label before we delete the node — after delete
	// the name is no longer in the store.
	displayLabel := nodeDisplayLabel(fid, nodeID)

	stepper.Step(fmt.Sprintf("Decommissioning %s", displayLabel))

	fabricIndex, removeErr := decommissionOnDevice(cmd.Context(), stepper, nodeID)
	if removeErr != nil {
		if !confirmForceDelete(cmd, stepper, force, removeErr) {
			return removeErr
		}
	} else {
		stepper.Step(fmt.Sprintf("Device dropped our fabric %s",
			output.Muted(fmt.Sprintf("(fabric index %d)", fabricIndex))))
	}

	if err := removeNode(fid, nodeID); err != nil {
		return fmt.Errorf("removing node %d from local store: %w", nodeID, err)
	}

	if removeErr != nil {
		stepper.Success(fmt.Sprintf("Removed %s from local store only", displayLabel))
		fmt.Fprintf(cmd.ErrOrStderr(), "%s Device was not notified and may still hold our fabric credentials.\n",
			output.WarningIcon())
		return nil
	}

	stepper.Success(fmt.Sprintf("Decommissioned %s", displayLabel))
	return nil
}

// nodeDisplayLabel returns "<name> (node N)" when a name exists, otherwise
// just "node N". The result is pre-styled for direct use in stepper output.
func nodeDisplayLabel(fabricID, nodeID uint64) string {
	if node, err := loadNodeForCompletion(fabricID, nodeID); err == nil && node.Name != "" {
		return output.Bold(node.Name) + " " + output.Muted(fmt.Sprintf("(node %d)", nodeID))
	}
	return output.Bold(fmt.Sprintf("node %d", nodeID))
}

// decommissionOnDevice reads the accessing fabric index from the device and
// sends RemoveFabric on OperationalCredentials, endpoint 0. Returns the fabric
// index that was removed, or a friendly error describing what went wrong.
// Both reads and invokes go through the session daemon when available so that
// a single cached CASE session is reused for both round-trips.
func decommissionOnDevice(ctx context.Context, stepper *output.Stepper, nodeID uint64) (uint8, error) {
	stepper.Step("Reading CurrentFabricIndex")
	fabricIndex, err := readCurrentFabricIndex(ctx, nodeID)
	if err != nil {
		return 0, fmt.Errorf("reading CurrentFabricIndex: %w", err)
	}
	if fabricIndex == 0 {
		return 0, fmt.Errorf("device reported CurrentFabricIndex=0 (no accessing fabric)")
	}

	payload, err := encodeRemoveFabricFields(fabricIndex)
	if err != nil {
		return 0, fmt.Errorf("encoding RemoveFabric: %w", err)
	}

	stepper.Step(fmt.Sprintf("Sending RemoveFabric %s",
		output.Muted(fmt.Sprintf("(index %d)", fabricIndex))))

	if err := invokeRemoveFabric(ctx, nodeID, payload); err != nil {
		return 0, err
	}
	return fabricIndex, nil
}

// readCurrentFabricIndex reads OperationalCredentials.CurrentFabricIndex on
// endpoint 0. The value returned is the accessing fabric index — i.e. our
// fabric's index on the device — which is exactly what RemoveFabric expects.
func readCurrentFabricIndex(ctx context.Context, nodeID uint64) (uint8, error) {
	const ep0 = uint16(0)

	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("decommission: reading CurrentFabricIndex via daemon", "node", nodeID)
		resp, err := dc.Read(daemon.AttrPathReq{
			Endpoint:    ep0,
			ClusterID:   operationalcredentials.ID,
			AttributeID: operationalcredentials.AttrCurrentFabricIndex,
		})
		if err != nil {
			return 0, err
		}
		if len(resp.Reports) == 0 {
			return 0, fmt.Errorf("no report data")
		}
		r := resp.Reports[0]
		if r.StatusCode != 0 {
			return 0, fmt.Errorf("status 0x%02X", r.StatusCode)
		}
		data, derr := daemon.DecodeFields(r.Data)
		if derr != nil {
			return 0, fmt.Errorf("decoding fields: %w", derr)
		}
		return decodeFabricIndex(data)
	}

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	reports, err := client.Read(ctx, session,
		interaction.NewAttributePath(ep0, operationalcredentials.ID, operationalcredentials.AttrCurrentFabricIndex))
	if err != nil {
		return 0, err
	}
	if len(reports) == 0 {
		return 0, fmt.Errorf("no report data")
	}
	r := reports[0]
	if r.Status != nil {
		return 0, fmt.Errorf("status 0x%02X", r.Status.Status.Status)
	}
	if r.Data != nil {
		return decodeFabricIndex(r.Data.Data)
	}
	return 0, fmt.Errorf("no report data")
}

// decodeFabricIndex reads a single uint TLV element and narrows it to uint8.
func decodeFabricIndex(data []byte) (uint8, error) {
	v, ok := decodeTLVUint(data)
	if !ok {
		return 0, fmt.Errorf("CurrentFabricIndex had unexpected TLV type")
	}
	if v > 0xFE {
		return 0, fmt.Errorf("CurrentFabricIndex %d is out of range", v)
	}
	return uint8(v), nil
}

// encodeRemoveFabricFields builds the TLV payload for
// OperationalCredentials.RemoveFabric. Tag 0: FabricIndex (uint8).
func encodeRemoveFabricFields(fabricIndex uint8) ([]byte, error) {
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.ContextTag(0), uint64(fabricIndex)); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// invokeRemoveFabric sends RemoveFabric on OperationalCredentials (endpoint 0)
// and maps the NOCResponse status into a user-friendly error. Preferring the
// session daemon lets us reuse the cached CASE session from the preceding
// CurrentFabricIndex read.
func invokeRemoveFabric(ctx context.Context, nodeID uint64, payload []byte) error {
	const (
		ep0       = uint16(0)
		clusterID = operationalcredentials.ID
		commandID = operationalcredentials.CmdRemoveFabric
	)

	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("decommission: invoking RemoveFabric via daemon", "node", nodeID)
		resp, err := dc.Invoke(ep0, clusterID, commandID, payload, 0)
		if err != nil {
			return fmt.Errorf("invoking RemoveFabric: %w", err)
		}
		if resp.StatusCode != 0 {
			return fmt.Errorf("RemoveFabric failed: IM status 0x%02X (%s)",
				resp.StatusCode, interaction.StatusCode(resp.StatusCode))
		}
		if resp.HasData {
			data, err := daemon.DecodeFields(resp.Data)
			if err != nil {
				return fmt.Errorf("decoding RemoveFabric response: %w", err)
			}
			return parseNOCResponse(data)
		}
		return nil
	}

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("connecting to node: %w", err)
	}
	defer cleanup()

	path := interaction.NewCommandPath(ep0, clusterID, commandID)
	resp, err := client.Invoke(ctx, session, path, payload)
	if err != nil {
		return fmt.Errorf("invoking RemoveFabric: %w", err)
	}
	if resp.Status != nil {
		st := resp.Status.Status.Status
		if st != 0 {
			return fmt.Errorf("RemoveFabric failed: IM status 0x%02X (%s)",
				st, interaction.StatusCode(st))
		}
	}
	if resp.Command != nil && len(resp.Command.Fields) > 0 {
		return parseNOCResponse(resp.Command.Fields)
	}
	return nil
}

// parseNOCResponse inspects an NOCResponse structure and returns nil on OK or
// a friendly error otherwise.
func parseNOCResponse(fields []byte) error {
	status, debug, err := decodeNOCResponse(fields)
	if err != nil {
		return fmt.Errorf("decoding NOCResponse: %w", err)
	}
	if status == nocStatusOK {
		return nil
	}
	msg := nocStatusString(status)
	if debug != "" {
		return fmt.Errorf("RemoveFabric rejected: %s — %s", msg, debug)
	}
	return fmt.Errorf("RemoveFabric rejected: %s", msg)
}

// decodeNOCResponse reads the NOCResponse command fields returning StatusCode
// (tag 0) and DebugText (tag 2). FabricIndex (tag 1) is not surfaced.
// Fields is the rawstruct body of the InvokeResponse — a sequence of
// tagged elements without an outer Structure wrapper.
func decodeNOCResponse(fields []byte) (uint8, string, error) {
	if len(fields) == 0 {
		return 0, "", fmt.Errorf("empty NOCResponse")
	}
	r := tlv.NewReader(bytes.NewReader(fields))

	var status uint8
	var debug string
	var hasStatus bool
	for {
		if err := r.Next(); err != nil {
			if err == io.EOF {
				break
			}
			return 0, "", fmt.Errorf("decoding NOCResponse TLV: %w", err)
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		switch r.TagValue().TagNum {
		case 0:
			if v, ok := r.Value().(uint64); ok {
				status = uint8(v)
				hasStatus = true
			}
		case 2:
			if v, ok := r.Value().(string); ok {
				debug = v
			}
		}
	}
	if !hasStatus {
		return 0, "", fmt.Errorf("NOCResponse missing mandatory StatusCode (tag 0)")
	}
	return status, debug, nil
}

// nocStatusString maps NodeOperationalCertStatusEnum values to a message.
func nocStatusString(status uint8) string {
	switch status {
	case nocStatusOK:
		return "OK"
	case nocStatusInvalidFabricIndex:
		return "InvalidFabricIndex — device did not recognize our fabric index (retry after `matter fabric ls` to confirm the device state)"
	default:
		return fmt.Sprintf("NOCStatus 0x%02X", status)
	}
}

// confirmForceDelete returns true when the caller should delete the local
// store entry despite a failed RemoveFabric. With --force we skip the prompt.
func confirmForceDelete(cmd *cobra.Command, stepper *output.Stepper, force bool, cause error) bool {
	stepper.Fail(cause.Error())

	if force {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s RemoveFabric failed; --force given, deleting local record anyway.\n",
			output.WarningIcon())
		return true
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "%s Device did not accept RemoveFabric: %v\n",
		output.WarningIcon(), cause)
	fmt.Fprint(cmd.ErrOrStderr(), "Remove from local store anyway? [y/N] ")

	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s Failed to read confirmation input: %v\n",
				output.WarningIcon(), err)
		}
		return false
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes"
}
