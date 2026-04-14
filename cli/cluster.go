// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/cli/completion"
	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/controller"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/tlv"

	// Import all generated cluster packages so they register via init().
	_ "github.com/p0fi/matter-cli/internal/clusters/all"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// shorthandCmds is the list of top-level shorthand cluster commands registered
// by registerShorthandClusters. filterShorthandCommands iterates this slice
// instead of rootCmd.Commands() to avoid a variable-initialization cycle.
var shorthandCmds []*cobra.Command

// allRootCommands returns all top-level commands. It is set in init() to avoid
// a variable-initialization cycle: rootCmd → PersistentPreRunE →
// filterShorthandCommands → rootCmd.
var allRootCommands func() []*cobra.Command

func init() {
	allRootCommands = func() []*cobra.Command { return rootCmd.Commands() }
	rootCmd.AddCommand(withGroup(newClusterCmd(), groupClusters))
	registerShorthandClusters()
}

// targetUnawareCommands lists top-level commands that do not make sense when
// a device target is already selected (e.g. @air-2000i/1). They are hidden
// from completion and help output whenever a target is resolved.
var targetUnawareCommands = map[string]bool{
	"commission":  true,
	"discover":    true,
	"fabric":      true,
	"use":         true,
	"completion":  true,
	"config":      true,
	"payload":     true,
	"session":     true,
	"version":     true,
	"interactive": true,
}

// deviceOnlyCommands lists top-level commands that are meaningful at the
// device (node) level but not when a specific endpoint is targeted. They are
// hidden from completion when an endpoint-explicit target is set.
var deviceOnlyCommands = map[string]bool{
	"tree": true,
}

// filterShorthandCommands hides commands that are irrelevant for the resolved
// target based on its specificity:
//
//   - Target-unaware commands (commission, fabric, discover, …) are hidden
//     whenever any target is set.
//   - Node-only target (ExplicitEndpoint=false): all cluster commands are
//     hidden; only device-level commands (device inspect, device alias) remain.
//   - Endpoint-explicit target (ExplicitEndpoint=true): device-only commands
//     are hidden; cluster commands are shown (filtered to those present on the
//     endpoint when the store is reachable).
//
// Nothing is hidden when the target lacks a node ID.
func filterShorthandCommands(t *Target) {
	if t == nil || t.NodeID == 0 {
		return
	}

	// Always hide management commands that don't apply to a device target.
	for _, cmd := range shorthandCmds {
		if targetUnawareCommands[cmd.Name()] {
			cmd.Hidden = true
		}
	}
	for _, cmd := range allRootCommands() {
		if targetUnawareCommands[cmd.Name()] {
			cmd.Hidden = true
		}
	}

	if !t.ExplicitEndpoint {
		// Node-only target: hide all cluster commands so only device-level
		// commands (device inspect, device alias) remain visible.
		for _, cmd := range shorthandCmds {
			cmd.Hidden = true
		}
		for _, cmd := range allRootCommands() {
			if cmd.GroupID == groupClusters {
				cmd.Hidden = true
			}
		}
		return
	}

	// Endpoint-explicit target: hide node-only commands, keep clusters.
	for _, cmd := range allRootCommands() {
		if deviceOnlyCommands[cmd.Name()] {
			cmd.Hidden = true
		}
	}

	// Filter shorthand cluster commands by endpoint if we know it.
	if !t.EndpointSet {
		return
	}

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	node, err := getNodeForCompletion(fabricID, t.NodeID)
	if err != nil {
		return
	}

	// Collect the server cluster IDs on the target endpoint.
	clusterIDs := make(map[uint32]bool)
	for _, ep := range node.Endpoints {
		if ep.ID == t.Endpoint {
			for _, cl := range ep.Clusters {
				if cl.Side == "server" || cl.Side == "" {
					clusterIDs[cl.ID] = true
				}
			}
			break
		}
	}
	if len(clusterIDs) == 0 {
		return
	}

	// Hide shorthand cluster commands whose cluster is absent from the endpoint.
	for _, cmd := range shorthandCmds {
		cl, ok := clusters.Global.ClusterByName(cmd.Name())
		if !ok {
			continue
		}
		if !clusterIDs[cl.ID] {
			cmd.Hidden = true
		}
	}
}

// connectToNode opens the store, looks up the node's last address, creates a
// controller, and establishes a CASE session. The returned cleanup function
// must be called when done.
func connectToNode(ctx context.Context, nodeID uint64) (
	*interaction.Client, *protocol.Session, func(), error,
) {
	s, err := openStore()
	if err != nil {
		return nil, nil, nil, err
	}

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	node, err := s.GetNode(fabricID, nodeID)
	if err != nil {
		s.Close()
		return nil, nil, nil, fmt.Errorf("looking up node %d: %w", nodeID, err)
	}
	if node.LastAddress == "" {
		s.Close()
		return nil, nil, nil, fmt.Errorf("node %d has no known address", nodeID)
	}

	ctrl, err := controller.New(controller.Config{
		Store:    s,
		FabricID: fabricID,
	})
	if err != nil {
		s.Close()
		return nil, nil, nil, fmt.Errorf("creating controller: %w", err)
	}

	session, err := ctrl.ConnectCASE(ctx, node.LastAddress, nodeID)
	if err != nil {
		ctrl.Close()
		s.Close()
		return nil, nil, nil, fmt.Errorf("establishing CASE session to node %d: %w", nodeID, err)
	}

	node.LastSeen = time.Now()
	if saveErr := s.SaveNode(fabricID, node); saveErr != nil {
		slog.Warn("failed to update LastSeen", "node", nodeID, "err", saveErr)
	}

	client := interaction.NewClient(ctrl.Exchanges())
	cleanup := func() {
		ctrl.Close()
		s.Close()
	}
	return client, session, cleanup, nil
}

// maxValueLen is the maximum display length for a formatted TLV value before
// it gets truncated.
const maxValueLen = 40

// formatAttrValue decodes raw TLV bytes and returns a display string.
// For bitmap types, the binary representation is appended (e.g. "11 (0b1011)").
func formatAttrValue(raw []byte, attrType string) string {
	value := decodeTLVValue(raw)
	if !strings.HasPrefix(attrType, "bitmap") {
		return value
	}
	// Parse the decimal value back so we can show it in binary.
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%d (0b%b)", n, n)
}

// displayReadValue formats and prints an attribute read result. For FeatureMap
// attributes on clusters with known features, it appends a detailed breakdown
// showing which feature flags are enabled and which are not.
func displayReadValue(cmd *cobra.Command, stepper *output.Stepper, cl *clusters.ClusterInfo, attr *clusters.AttributeInfo, data []byte) {
	if attr.ID == 0xFFFC && len(cl.Features) > 0 {
		if n, ok := decodeTLVUint(data); ok {
			stepper.Success(fmt.Sprintf("%s/%s = %s",
				output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
				output.Success(fmt.Sprintf("%d (0b%b)", n, n))))
			printFeatureMap(cmd.OutOrStdout(), cl.Features, uint32(n))
			return
		}
	}
	value := formatAttrValue(data, attr.Type)
	stepper.Success(fmt.Sprintf("%s/%s = %s",
		output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
		output.Success(value)))
}

// decodeTLVUint decodes a single unsigned integer from raw TLV bytes.
func decodeTLVUint(raw []byte) (uint64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	r := tlv.NewReader(bytes.NewReader(raw))
	if err := r.Next(); err != nil {
		return 0, false
	}
	if v, ok := r.Value().(uint64); ok {
		return v, true
	}
	return 0, false
}

// printFeatureMap writes a list of feature flags showing which are enabled
// and which are disabled based on the bitmap value.
func printFeatureMap(w io.Writer, features []clusters.FeatureInfo, value uint32) {
	fmt.Fprintf(w, "\n  %s\n", output.Bold("Feature Flags"))
	for _, f := range features {
		enabled := value&(1<<f.Bit) != 0
		if enabled {
			fmt.Fprintf(w, "    %s %s %s\n",
				output.Success("✓"), f.Name,
				output.Muted("("+f.Code+")"))
		} else {
			fmt.Fprintf(w, "    %s %s %s\n",
				output.Muted("✗"), output.Muted(f.Name),
				output.Muted("("+f.Code+")"))
		}
	}
}

// decodeTLVValue reads a single TLV element from raw bytes and returns a
// human-readable string representation of the value.
func decodeTLVValue(raw []byte) string {
	if len(raw) == 0 {
		return "<empty>"
	}
	r := tlv.NewReader(bytes.NewReader(raw))
	if err := r.Next(); err != nil {
		return fmt.Sprintf("0x%s", hex.EncodeToString(raw))
	}
	return formatTLVElement(r)
}

// formatTLVElement formats the current TLV element (after Next() has been called).
func formatTLVElement(r *tlv.Reader) string {
	t := r.Type()

	// Handle container types by iterating their elements.
	if t == tlv.TypeArray || t == tlv.TypeList || t == tlv.TypeStructure {
		return formatTLVContainer(r, t)
	}

	v := r.Value()
	switch val := v.(type) {
	case bool:
		if val {
			return "true"
		}
		return "false"
	case uint64:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float32:
		return fmt.Sprintf("%g", val)
	case float64:
		return fmt.Sprintf("%g", val)
	case string:
		return truncateMiddle(fmt.Sprintf("%q", val))
	case []byte:
		return truncateMiddle(fmt.Sprintf("0x%s", hex.EncodeToString(val)))
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatTLVContainer formats an array, list, or structure container.
// Long containers are truncated to show the first and last element with
// an ellipsis in between, e.g. [0, 1, ..., 65533].
func formatTLVContainer(r *tlv.Reader, ct tlv.ElementType) string {
	isStruct := ct == tlv.TypeStructure
	open, close := "[", "]"
	if isStruct {
		open, close = "{", "}"
	}

	var parts []string
	for {
		if err := r.Next(); err != nil {
			break
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}

		elem := formatTLVElement(r)
		if isStruct {
			tag := r.TagValue()
			elem = fmt.Sprintf("%d: %s", tag.TagNum, elem)
		}
		parts = append(parts, elem)
	}

	full := open + strings.Join(parts, ", ") + close
	if len(full) <= maxValueLen || len(parts) <= 2 {
		return full
	}

	// Truncate: show first elements, ..., last element.
	last := parts[len(parts)-1]
	var truncated string
	for i := 1; i < len(parts)-1; i++ {
		candidate := open + strings.Join(parts[:i], ", ") + ", ..., " + last + close
		if len(candidate) > maxValueLen {
			break
		}
		truncated = candidate
	}
	if truncated == "" {
		truncated = open + parts[0] + ", ..., " + last + close
	}
	return truncated
}

// truncateMiddle truncates a string that exceeds maxValueLen by replacing
// the middle portion with "...".
func truncateMiddle(s string) string {
	if len(s) <= maxValueLen {
		return s
	}
	// Keep slightly more of the prefix than the suffix.
	keep := maxValueLen - 3 // account for "..."
	pre := keep/2 + keep%2
	suf := keep / 2
	return s[:pre] + "..." + s[len(s)-suf:]
}

// newClusterCmd creates the `matter-cli cluster` subcommand group.
func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Interact with device clusters (read, write, invoke, subscribe)",
	}
	cmd.AddCommand(newClusterReadCmd())
	cmd.AddCommand(newClusterWriteCmd())
	cmd.AddCommand(newClusterInvokeCmd())
	cmd.AddCommand(newClusterSubscribeCmd())
	cmd.AddCommand(newClusterListCmd())
	return cmd
}

func newClusterReadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read",
		Short: "Read a cluster attribute",
		Example: `  matter @1/1 cluster read --cluster OnOff --attribute OnOff
  matter @kitchen cluster read --cluster LevelControl --attribute CurrentLevel`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, endpoint, err := requireTarget(cmd)
			if err != nil {
				return err
			}
			clusterName, _ := cmd.Flags().GetString("cluster")
			attrName, _ := cmd.Flags().GetString("attribute")

			if clusterName == "" {
				return fmt.Errorf("--cluster is required")
			}
			if attrName == "" {
				return fmt.Errorf("--attribute is required")
			}

			cl, ok := clusters.Global.ClusterByName(clusterName)
			if !ok {
				return fmt.Errorf("unknown cluster %q", clusterName)
			}
			attr, ok := clusters.Global.AttributeByName(cl.ID, attrName)
			if !ok {
				return fmt.Errorf("unknown attribute %q in cluster %q", attrName, clusterName)
			}

			return readAttribute(cmd, nodeID, endpoint, cl, attr)
		},
	}
	cmd.Flags().String("cluster", "", "cluster name or ID")
	cmd.Flags().String("attribute", "", "attribute name or ID")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completion.ClusterNameCompletion(clusters.Global))
	_ = cmd.RegisterFlagCompletionFunc("attribute", completion.AttributeNameCompletion(clusters.Global))
	return cmd
}

// readAttribute performs the actual attribute read over CASE and displays the result.
func readAttribute(cmd *cobra.Command, nodeID uint64, endpoint uint16, cl *clusters.ClusterInfo, attr *clusters.AttributeInfo) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	stepper.Step(fmt.Sprintf("Reading %s/%s from %s endpoint %s %s",
		output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
		output.Bold(resolveNodeLabel(nodeID)), output.Bold(fmt.Sprintf("%d", endpoint)),
		output.Muted(fmt.Sprintf("(0x%04X/0x%04X)", cl.ID, attr.ID))))

	// Try the daemon first for faster session reuse.
	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("using session daemon for read", "node", nodeID)
		stepper.Step("Sending read request " + output.Muted("(via session daemon)"))
		dresp, err := dc.Read(daemon.AttrPathReq{
			Endpoint:    endpoint,
			ClusterID:   cl.ID,
			AttributeID: attr.ID,
		})
		if err != nil {
			stepper.Fail(fmt.Sprintf("Read failed: %v", err))
			return fmt.Errorf("reading attribute: %w", err)
		}
		for _, r := range dresp.Reports {
			if r.StatusCode != 0 {
				stepper.Fail(fmt.Sprintf("Read error: status 0x%02X", r.StatusCode))
				return fmt.Errorf("read error: status 0x%02X", r.StatusCode)
			}
			data, _ := daemon.DecodeFields(r.Data)
			if len(data) > 0 {
				displayReadValue(cmd, stepper, cl, attr, data)
				return nil
			}
		}
		stepper.Success(fmt.Sprintf("%s/%s = <no data>",
			output.Bold(cl.DisplayName), output.Info(attr.DisplayName)))
		return nil
	}

	// No daemon available — establish a direct CASE session.
	ctx := cmd.Context()
	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Failed to connect: %v", err))
		return err
	}
	defer cleanup()

	stepper.Step("Sending read request")
	path := interaction.NewAttributePath(endpoint, cl.ID, attr.ID)
	reports, err := client.Read(ctx, session, path)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Read failed: %v", err))
		return fmt.Errorf("reading attribute: %w", err)
	}

	for _, r := range reports {
		if r.Status != nil {
			stepper.Fail(fmt.Sprintf("Read error: status 0x%02X", r.Status.Status.Status))
			return fmt.Errorf("read error: status 0x%02X", r.Status.Status.Status)
		}
		if r.Data != nil {
			displayReadValue(cmd, stepper, cl, attr, r.Data.Data)
			return nil
		}
	}

	stepper.Success(fmt.Sprintf("%s/%s = <no data>",
		output.Bold(cl.DisplayName), output.Info(attr.DisplayName)))
	return nil
}

// invokeCommand performs the actual command invoke over CASE and displays the result.
func invokeCommand(cmd *cobra.Command, nodeID uint64, endpoint uint16, cl *clusters.ClusterInfo, ci *clusters.CommandInfo) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	stepper.Step(fmt.Sprintf("Invoking %s/%s on %s endpoint %s %s",
		output.Bold(cl.DisplayName), output.Info(ci.DisplayName),
		output.Bold(resolveNodeLabel(nodeID)), output.Bold(fmt.Sprintf("%d", endpoint)),
		output.Muted(fmt.Sprintf("(0x%04X/0x%04X)", cl.ID, ci.ID))))

	// Build command fields from --field flags and/or positional args.
	var fields []byte
	if ci.HasRequest && len(ci.RequestFields) > 0 {
		fieldMap, err := collectCommandFields(cmd, ci)
		if err != nil {
			stepper.Fail(fmt.Sprintf("Invalid fields: %v", err))
			return err
		}

		// Validate that all required fields are provided.
		for _, rf := range ci.RequiredFields() {
			if _, ok := fieldMap[rf.Name]; !ok {
				var fieldNames []string
				for _, f := range ci.RequestFields {
					opt := ""
					if f.Optional {
						opt = " (optional)"
					}
					fieldNames = append(fieldNames, fmt.Sprintf("  %s (%s)%s", f.Name, f.Type, opt))
				}
				stepper.Fail(fmt.Sprintf("Missing required field %q", rf.Name))
				return fmt.Errorf("missing required field %q for command %s\n\nExpected fields:\n%s\n\nUsage:\n  --field %s=<value>",
					rf.Name, ci.DisplayName, strings.Join(fieldNames, "\n"), rf.Name)
			}
		}

		fields, err = encodeCommandFields(ci, fieldMap)
		if err != nil {
			stepper.Fail(fmt.Sprintf("Failed to encode fields: %v", err))
			return fmt.Errorf("encoding command fields: %w", err)
		}
	}

	// Try the daemon first for faster session reuse.
	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("using session daemon for invoke", "node", nodeID)
		stepper.Step("Sending invoke request " + output.Muted("(via session daemon)"))
		dresp, err := dc.Invoke(endpoint, cl.ID, ci.ID, fields, 0)
		if err != nil {
			stepper.Fail(fmt.Sprintf("Invoke failed: %v", err))
			return fmt.Errorf("invoking command: %w", err)
		}
		if dresp.StatusCode != 0 {
			stepper.Fail(fmt.Sprintf("Command failed with status 0x%02X", dresp.StatusCode))
			return fmt.Errorf("command failed with status 0x%02X", dresp.StatusCode)
		}
		if dresp.HasData {
			data, _ := daemon.DecodeFields(dresp.Data)
			stepper.Success(fmt.Sprintf("Response: %s", decodeTLVValue(data)))
		} else {
			stepper.Success("Success")
		}
		return nil
	}

	// No daemon available — establish a direct CASE session.
	ctx := cmd.Context()
	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Failed to connect: %v", err))
		return err
	}
	defer cleanup()

	stepper.Step("Sending invoke request")
	path := interaction.NewCommandPath(endpoint, cl.ID, ci.ID)
	resp, err := client.Invoke(ctx, session, path, fields)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Invoke failed: %v", err))
		return fmt.Errorf("invoking command: %w", err)
	}

	if resp.Status != nil && resp.Status.Status.Status != 0 {
		stepper.Fail(fmt.Sprintf("Command failed with status 0x%02X", resp.Status.Status.Status))
		return fmt.Errorf("command failed with status 0x%02X", resp.Status.Status.Status)
	}

	if resp.Command != nil && len(resp.Command.Fields) > 0 {
		stepper.Success(fmt.Sprintf("Response: %s", decodeTLVValue(resp.Command.Fields)))
	} else {
		stepper.Success("Success")
	}
	return nil
}

// collectCommandFields gathers field values from --field flags and positional
// args (for shorthand commands). It returns a map of field-name → string-value.
func collectCommandFields(cmd *cobra.Command, ci *clusters.CommandInfo) (map[string]string, error) {
	fieldMap := make(map[string]string)

	// Collect from --field / -F flags.
	if cmd.Flags().Lookup("field") != nil {
		fieldFlags, _ := cmd.Flags().GetStringSlice("field")
		for _, kv := range fieldFlags {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid --field format %q, expected key=value", kv)
			}
			name := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if _, ok := ci.FieldByName(name); !ok {
				var valid []string
				for _, f := range ci.RequestFields {
					valid = append(valid, f.Name)
				}
				return nil, fmt.Errorf("unknown field %q, valid fields: %s", name, strings.Join(valid, ", "))
			}
			fieldMap[name] = value
		}
	}

	// Collect from positional args (for shorthand commands).
	// Each arg can be either a bare value (mapped to the next unset field in
	// order) or an explicit Key=Value pair.
	args := cmd.Flags().Args()
	if len(args) > 0 {
		posIdx := 0 // tracks the next positional RequestField slot
		for _, arg := range args {
			if name, value, ok := strings.Cut(arg, "="); ok {
				name = strings.TrimSpace(name)
				if _, found := ci.FieldByName(name); !found {
					var valid []string
					for _, f := range ci.RequestFields {
						valid = append(valid, f.Name)
					}
					return nil, fmt.Errorf("unknown field %q, valid fields: %s", name, strings.Join(valid, ", "))
				}
				fieldMap[name] = strings.TrimSpace(value)
			} else {
				// Bare positional value — assign to the next unset field.
				for posIdx < len(ci.RequestFields) {
					f := ci.RequestFields[posIdx]
					posIdx++
					if _, already := fieldMap[f.Name]; !already {
						fieldMap[f.Name] = arg
						break
					}
				}
				if posIdx > len(ci.RequestFields) {
					return nil, fmt.Errorf("too many positional arguments: expected at most %d for fields %s",
						len(ci.RequestFields), commandFieldUsage(ci))
				}
			}
		}
	}

	return fieldMap, nil
}

// encodeCommandFields encodes a set of field name=value pairs into TLV bytes
// suitable for passing as the command fields payload. Each field is encoded
// with its context tag as defined in the CommandFieldInfo.
func encodeCommandFields(ci *clusters.CommandInfo, fieldMap map[string]string) ([]byte, error) {
	w := tlv.NewWriter()
	for _, f := range ci.RequestFields {
		val, ok := fieldMap[f.Name]
		if !ok {
			if f.Optional {
				continue
			}
			return nil, fmt.Errorf("missing required field %q", f.Name)
		}
		// Resolve enum name to numeric value (e.g. "Increase" → "0").
		if len(f.EnumValues) > 0 {
			for _, ev := range f.EnumValues {
				if strings.EqualFold(ev.Name, val) {
					val = fmt.Sprintf("%d", ev.Value)
					break
				}
			}
		}
		tag := tlv.ContextTag(f.ID)
		if err := encodeTaggedValue(w, tag, f.Type, val); err != nil {
			return nil, fmt.Errorf("encoding field %q: %w", f.Name, err)
		}
	}
	return w.Bytes(), nil
}

// encodeTaggedValue writes a single TLV element with the given tag, type, and
// string value to the writer.
func encodeTaggedValue(w *tlv.Writer, tag tlv.Tag, fieldType, value string) error {
	switch fieldType {
	case "bool":
		v := strings.ToLower(strings.TrimSpace(value))
		switch v {
		case "true", "1", "on", "yes":
			return w.PutBool(tag, true)
		case "false", "0", "off", "no":
			return w.PutBool(tag, false)
		default:
			return fmt.Errorf("invalid bool value %q (use true/false)", value)
		}

	case "uint8", "uint16", "uint32", "uint64", "enum8", "enum16", "bitmap8", "bitmap16", "bitmap32":
		v, err := strconv.ParseUint(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return fmt.Errorf("invalid unsigned integer %q: %w", value, err)
		}
		return w.PutUnsignedInt(tag, v)

	case "int8", "int16", "int32", "int64":
		v, err := strconv.ParseInt(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return fmt.Errorf("invalid signed integer %q: %w", value, err)
		}
		return w.PutSignedInt(tag, v)

	case "float32":
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
		if err != nil {
			return fmt.Errorf("invalid float %q: %w", value, err)
		}
		return w.PutFloat32(tag, float32(v))

	case "float64":
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return fmt.Errorf("invalid float %q: %w", value, err)
		}
		return w.PutFloat64(tag, v)

	case "string", "utf8":
		return w.PutUTF8String(tag, value)

	case "octets", "bytes":
		b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
		if err != nil {
			return fmt.Errorf("invalid hex bytes %q: %w", value, err)
		}
		return w.PutOctetString(tag, b)

	default:
		// Fallback: try parsing as unsigned integer.
		v, err := strconv.ParseUint(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return fmt.Errorf("unsupported field type %q and value %q is not a valid integer", fieldType, value)
		}
		return w.PutUnsignedInt(tag, v)
	}
}

// commandFieldUsage returns a human-readable usage hint for a command's fields.
func commandFieldUsage(ci *clusters.CommandInfo) string {
	var parts []string
	for _, f := range ci.RequestFields {
		if f.Optional {
			parts = append(parts, fmt.Sprintf("[%s]", f.Name))
		} else {
			parts = append(parts, fmt.Sprintf("<%s>", f.Name))
		}
	}
	return strings.Join(parts, " ")
}

func newClusterWriteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write a cluster attribute",
		Example: `  matter @1/1 cluster write --cluster OnOff --attribute OnTime --value 300
  matter @kitchen cluster write --cluster FanControl --attribute FanMode --value 0`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, endpoint, err := requireTarget(cmd)
			if err != nil {
				return err
			}
			clusterName, _ := cmd.Flags().GetString("cluster")
			attrName, _ := cmd.Flags().GetString("attribute")
			value, _ := cmd.Flags().GetString("value")

			if clusterName == "" {
				return fmt.Errorf("--cluster is required")
			}
			if attrName == "" {
				return fmt.Errorf("--attribute is required")
			}

			cl, ok := clusters.Global.ClusterByName(clusterName)
			if !ok {
				return fmt.Errorf("unknown cluster %q", clusterName)
			}
			attr, ok := clusters.Global.AttributeByName(cl.ID, attrName)
			if !ok {
				return fmt.Errorf("unknown attribute %q in cluster %q", attrName, clusterName)
			}
			if !attr.Writable {
				return fmt.Errorf("attribute %q is read-only", attrName)
			}

			return writeAttribute(cmd, nodeID, endpoint, cl, attr, value)
		},
	}
	cmd.Flags().String("cluster", "", "cluster name or ID")
	cmd.Flags().String("attribute", "", "attribute name or ID")
	cmd.Flags().String("value", "", "value to write")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completion.ClusterNameCompletion(clusters.Global))
	_ = cmd.RegisterFlagCompletionFunc("attribute", completion.AttributeNameCompletion(clusters.Global))
	return cmd
}

// writeAttribute performs the actual attribute write over CASE and displays the result.
func writeAttribute(cmd *cobra.Command, nodeID uint64, endpoint uint16, cl *clusters.ClusterInfo, attr *clusters.AttributeInfo, value string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	stepper.Step(fmt.Sprintf("Writing %s/%s = %s on %s endpoint %s %s",
		output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
		output.Success(value),
		output.Bold(resolveNodeLabel(nodeID)), output.Bold(fmt.Sprintf("%d", endpoint)),
		output.Muted(fmt.Sprintf("(0x%04X/0x%04X)", cl.ID, attr.ID))))

	tlvData, err := encodeTLVValue(attr.Type, value)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Invalid value: %v", err))
		return fmt.Errorf("encoding value: %w", err)
	}

	// Try the daemon first for faster session reuse.
	if dc, ok := connectViaDaemon(nodeID); ok {
		slog.Debug("using session daemon for write", "node", nodeID)
		stepper.Step("Sending write request " + output.Muted("(via session daemon)"))
		dresp, err := dc.Write(daemon.AttrWriteReq{
			Endpoint:    endpoint,
			ClusterID:   cl.ID,
			AttributeID: attr.ID,
			Data:        daemon.EncodeFields(tlvData),
		})
		if err != nil {
			stepper.Fail(fmt.Sprintf("Write failed: %v", err))
			return fmt.Errorf("writing attribute: %w", err)
		}
		for _, st := range dresp.Statuses {
			if st.StatusCode != 0 {
				stepper.Fail(fmt.Sprintf("Write error: status 0x%02X", st.StatusCode))
				return fmt.Errorf("write error: status 0x%02X", st.StatusCode)
			}
		}
		stepper.Success(fmt.Sprintf("%s/%s written",
			output.Bold(cl.DisplayName), output.Info(attr.DisplayName)))
		return nil
	}

	// No daemon available — establish a direct CASE session.
	ctx := cmd.Context()
	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Failed to connect: %v", err))
		return err
	}
	defer cleanup()

	stepper.Step("Sending write request")
	path := interaction.NewAttributePath(endpoint, cl.ID, attr.ID)
	write := interaction.AttributeWrite{
		Path: path,
		Data: tlvData,
	}

	statuses, err := client.Write(ctx, session, write)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Write failed: %v", err))
		return fmt.Errorf("writing attribute: %w", err)
	}

	for _, st := range statuses {
		if st.Status.Status != 0 {
			stepper.Fail(fmt.Sprintf("Write error: status 0x%02X", st.Status.Status))
			return fmt.Errorf("write error: status 0x%02X", st.Status.Status)
		}
	}

	stepper.Success(fmt.Sprintf("%s/%s = %s",
		output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
		output.Success(value)))
	return nil
}

// subscribeAttribute establishes a subscription to an attribute on a remote
// node and streams reports to stdout until the context is cancelled (Ctrl+C)
// or an error occurs.
func subscribeAttribute(cmd *cobra.Command, nodeID uint64, endpoint uint16, cl *clusters.ClusterInfo, attr *clusters.AttributeInfo, minInterval, maxInterval uint16) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	stepper := output.NewStepper(cmd.OutOrStdout(), verbose)

	stepper.Step(fmt.Sprintf("Subscribing to %s/%s on %s endpoint %s %s",
		output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
		output.Bold(resolveNodeLabel(nodeID)), output.Bold(fmt.Sprintf("%d", endpoint)),
		output.Muted(fmt.Sprintf("(0x%04X/0x%04X) [%d..%d]s", cl.ID, attr.ID, minInterval, maxInterval))))

	// Intercept SIGINT so Ctrl+C cancels the subscription cleanly.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	client, session, cleanup, err := connectToNode(ctx, nodeID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Failed to connect: %v", err))
		return err
	}
	defer cleanup()

	path := interaction.NewAttributePath(endpoint, cl.ID, attr.ID)
	sub, err := client.Subscribe(ctx, session, []interaction.AttributePath{path}, minInterval, maxInterval)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Subscribe failed: %v", err))
		return fmt.Errorf("subscribing to attribute: %w", err)
	}
	defer sub.Cancel()

	stepper.Success(fmt.Sprintf("Subscribed (ID %d) — streaming changes, Ctrl+C to stop", sub.ID))

	for {
		select {
		case reports, ok := <-sub.Reports:
			if !ok {
				return nil
			}
			for _, r := range reports {
				if r.Data != nil {
					value := decodeTLVValue(r.Data.Data)
					fmt.Fprintf(cmd.OutOrStdout(), "  %s/%s = %s\n",
						output.Bold(cl.DisplayName), output.Info(attr.DisplayName),
						output.Success(value))
				}
			}
		case err, ok := <-sub.Errors:
			if !ok {
				return nil
			}
			return fmt.Errorf("subscription error: %w", err)
		case <-ctx.Done():
			return nil
		}
	}
}

// encodeTLVValue encodes a string value into raw TLV bytes based on the attribute type.
func encodeTLVValue(attrType, value string) ([]byte, error) {
	w := tlv.NewWriter()

	switch attrType {
	case "bool":
		v := strings.ToLower(strings.TrimSpace(value))
		switch v {
		case "true", "1", "on", "yes":
			if err := w.PutBool(tlv.AnonymousTag(), true); err != nil {
				return nil, err
			}
		case "false", "0", "off", "no":
			if err := w.PutBool(tlv.AnonymousTag(), false); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("invalid bool value %q (use true/false)", value)
		}

	case "uint8", "uint16", "uint32", "uint64", "enum8", "enum16", "bitmap8", "bitmap16", "bitmap32":
		v, err := strconv.ParseUint(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid unsigned integer %q: %w", value, err)
		}
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), v); err != nil {
			return nil, err
		}

	case "int8", "int16", "int32", "int64":
		v, err := strconv.ParseInt(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid signed integer %q: %w", value, err)
		}
		if err := w.PutSignedInt(tlv.AnonymousTag(), v); err != nil {
			return nil, err
		}

	case "float32":
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q: %w", value, err)
		}
		if err := w.PutFloat32(tlv.AnonymousTag(), float32(v)); err != nil {
			return nil, err
		}

	case "float64":
		v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q: %w", value, err)
		}
		if err := w.PutFloat64(tlv.AnonymousTag(), v); err != nil {
			return nil, err
		}

	case "string", "utf8":
		if err := w.PutUTF8String(tlv.AnonymousTag(), value); err != nil {
			return nil, err
		}

	case "octets", "bytes":
		b, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid hex bytes %q: %w", value, err)
		}
		if err := w.PutOctetString(tlv.AnonymousTag(), b); err != nil {
			return nil, err
		}

	default:
		// Fall back: try parsing as unsigned integer.
		v, err := strconv.ParseUint(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("unsupported attribute type %q and value %q is not a valid integer", attrType, value)
		}
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), v); err != nil {
			return nil, err
		}
	}

	return w.Bytes(), nil
}

func newClusterInvokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "invoke",
		Short: "Invoke a cluster command",
		Example: `  matter @1/1 cluster invoke --cluster OnOff --command Toggle
  matter @kitchen cluster invoke --cluster Identify --command Identify -F IdentifyTime=10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, endpoint, err := requireTarget(cmd)
			if err != nil {
				return err
			}
			clusterName, _ := cmd.Flags().GetString("cluster")
			commandName, _ := cmd.Flags().GetString("command")

			if clusterName == "" {
				return fmt.Errorf("--cluster is required")
			}
			if commandName == "" {
				return fmt.Errorf("--command is required")
			}

			cl, ok := clusters.Global.ClusterByName(clusterName)
			if !ok {
				return fmt.Errorf("unknown cluster %q", clusterName)
			}
			cmdInfo, ok := clusters.Global.CommandByName(cl.ID, commandName)
			if !ok {
				return fmt.Errorf("unknown command %q in cluster %q", commandName, clusterName)
			}

			return invokeCommand(cmd, nodeID, endpoint, cl, cmdInfo)
		},
	}
	cmd.Flags().String("cluster", "", "cluster name or ID")
	cmd.Flags().String("command", "", "command name or ID")
	cmd.Flags().StringSliceP("field", "F", nil, "command field as key=value (repeatable, e.g. -F IdentifyTime=10)")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completion.ClusterNameCompletion(clusters.Global))
	_ = cmd.RegisterFlagCompletionFunc("command", completion.CommandNameCompletion(clusters.Global))
	_ = cmd.RegisterFlagCompletionFunc("field", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		commandName, _ := cmd.Flags().GetString("command")
		cl, ok := clusters.Global.ClusterByName(clusterName)
		if !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ci, ok := clusters.Global.CommandByName(cl.ID, commandName)
		if !ok {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		existing, _ := cmd.Flags().GetStringSlice("field")
		return completion.FieldFlagCompletions(ci.RequestFields, existing, toComplete)
	})
	return cmd
}

func newClusterSubscribeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subscribe",
		Short: "Subscribe to cluster attribute changes",
		Example: `  matter @1/1 cluster subscribe --cluster OnOff --attribute OnOff --min 1 --max 10
  matter @kitchen cluster subscribe --cluster OnOff --attribute OnOff --min 1 --max 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, endpoint, err := requireTarget(cmd)
			if err != nil {
				return err
			}
			clusterName, _ := cmd.Flags().GetString("cluster")
			attrName, _ := cmd.Flags().GetString("attribute")
			minInt, _ := cmd.Flags().GetUint16("min")
			maxInt, _ := cmd.Flags().GetUint16("max")

			if clusterName == "" {
				return fmt.Errorf("--cluster is required")
			}
			if attrName == "" {
				return fmt.Errorf("--attribute is required")
			}

			cl, ok := clusters.Global.ClusterByName(clusterName)
			if !ok {
				return fmt.Errorf("unknown cluster %q", clusterName)
			}
			attr, ok := clusters.Global.AttributeByName(cl.ID, attrName)
			if !ok {
				return fmt.Errorf("unknown attribute %q in cluster %q", attrName, clusterName)
			}

			return subscribeAttribute(cmd, nodeID, endpoint, cl, attr, minInt, maxInt)
		},
	}
	cmd.Flags().String("cluster", "", "cluster name or ID")
	cmd.Flags().String("attribute", "", "attribute name or ID")
	cmd.Flags().Uint16("min", 1, "minimum reporting interval in seconds")
	cmd.Flags().Uint16("max", 10, "maximum reporting interval in seconds")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completion.ClusterNameCompletion(clusters.Global))
	_ = cmd.RegisterFlagCompletionFunc("attribute", completion.AttributeNameCompletion(clusters.Global))
	return cmd
}

func newClusterListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all known clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			all := clusters.Global.AllClusters()
			format, _ := cmd.Flags().GetString("format")
			f := output.New(format)

			if _, ok := f.(*output.TableFormatter); ok {
				td := &output.TableData{
					Headers: []string{"ID", "NAME", "DISPLAY NAME", "ATTRS", "CMDS"},
				}
				for _, c := range all {
					td.Rows = append(td.Rows, []string{
						fmt.Sprintf("0x%04X", c.ID),
						c.Name,
						c.DisplayName,
						fmt.Sprintf("%d", len(c.Attributes)),
						fmt.Sprintf("%d", len(c.Commands)),
					})
				}
				return f.Format(cmd.OutOrStdout(), td)
			}
			return f.Format(cmd.OutOrStdout(), all)
		},
	}
}

// registerShorthandClusters adds top-level shorthand commands for each
// registered cluster. For example, `matter-cli on-off toggle` is a shorthand
// for `matter-cli cluster invoke --cluster on-off --command toggle`.
func registerShorthandClusters() {
	for _, cl := range clusters.Global.AllClusters() {
		clCopy := cl
		cmd := &cobra.Command{
			Use:         clCopy.Name,
			Short:       fmt.Sprintf("Shorthand commands for %s cluster", clCopy.DisplayName),
			Annotations: map[string]string{"shorthand-cluster": "true"},
		}

		// Add a subcommand for each command in the cluster.
		for _, ci := range clCopy.Commands {
			ciCopy := ci
			use := ciCopy.Name
			if ciCopy.HasRequest && len(ciCopy.RequestFields) > 0 {
				use = fmt.Sprintf("%s %s", ciCopy.Name, commandFieldUsage(&ciCopy))
			}
			sub := &cobra.Command{
				Use:   use,
				Short: fmt.Sprintf("Invoke %s.%s", clCopy.DisplayName, ciCopy.DisplayName),
				Args:  cobra.ArbitraryArgs,
				ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
					if len(ciCopy.RequestFields) == 0 {
						return nil, cobra.ShellCompDirectiveNoFileComp
					}
					return completion.FieldFlagCompletions(ciCopy.RequestFields, args, toComplete)
				},
				RunE: func(cmd *cobra.Command, args []string) error {
					nodeID, endpoint, err := requireTarget(cmd)
					if err != nil {
						return err
					}
					return invokeCommand(cmd, nodeID, endpoint, &clCopy, &ciCopy)
				},
			}
			if ciCopy.HasRequest && len(ciCopy.RequestFields) > 0 {
				sub.Flags().StringSliceP("field", "F", nil, "command field as key=value (repeatable)")
				_ = sub.RegisterFlagCompletionFunc("field", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
					existing, _ := cmd.Flags().GetStringSlice("field")
					return completion.FieldFlagCompletions(ciCopy.RequestFields, existing, toComplete)
				})
			}
			cmd.AddCommand(sub)
		}

		// Add a "read" subcommand for reading attributes.
		readCmd := &cobra.Command{
			Use:   "read <attribute>",
			Short: fmt.Sprintf("Read a %s attribute", clCopy.DisplayName),
			Args:  cobra.ExactArgs(1),
			ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				if len(args) >= 1 {
					return nil, cobra.ShellCompDirectiveNoFileComp
				}
				results := clusters.Global.SearchAttributes(clCopy.ID, toComplete)
				names := make([]string, len(results))
				for i, a := range results {
					names[i] = fmt.Sprintf("%s\t%s (0x%04X)", a.Name, a.DisplayName, a.ID)
				}
				return names, cobra.ShellCompDirectiveNoFileComp
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				nodeID, endpoint, err := requireTarget(cmd)
				if err != nil {
					return err
				}
				attrName := args[0]
				attr, ok := clusters.Global.AttributeByName(clCopy.ID, attrName)
				if !ok {
					return fmt.Errorf("unknown attribute %q in cluster %q", attrName, clCopy.Name)
				}
				return readAttribute(cmd, nodeID, endpoint, &clCopy, attr)
			},
		}
		cmd.AddCommand(readCmd)

		// Add a "write" subcommand for writing attributes.
		writeCmd := &cobra.Command{
			Use:   "write <attribute> <value>",
			Short: fmt.Sprintf("Write a %s attribute", clCopy.DisplayName),
			Args:  cobra.ExactArgs(2),
			ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				if len(args) == 0 {
					// Complete attribute names (only writable ones).
					results := clusters.Global.SearchAttributes(clCopy.ID, toComplete)
					var names []string
					for _, a := range results {
						if a.Writable {
							names = append(names, fmt.Sprintf("%s\t%s (0x%04X)", a.Name, a.DisplayName, a.ID))
						}
					}
					return names, cobra.ShellCompDirectiveNoFileComp
				}
				return nil, cobra.ShellCompDirectiveNoFileComp
			},
			RunE: func(cmd *cobra.Command, args []string) error {
				nodeID, endpoint, err := requireTarget(cmd)
				if err != nil {
					return err
				}
				attrName := args[0]
				attr, ok := clusters.Global.AttributeByName(clCopy.ID, attrName)
				if !ok {
					return fmt.Errorf("unknown attribute %q in cluster %q", attrName, clCopy.Name)
				}
				if !attr.Writable {
					return fmt.Errorf("attribute %q is read-only", attrName)
				}
				return writeAttribute(cmd, nodeID, endpoint, &clCopy, attr, args[1])
			},
		}
		cmd.AddCommand(writeCmd)

		rootCmd.AddCommand(withGroup(cmd, groupClusters))
		shorthandCmds = append(shorthandCmds, cmd)
	}
}
