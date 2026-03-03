// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/protocol"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/p0fi/matter-cli/internal/tlv"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// attrListAttrID is the Matter GlobalAttributeID for AttributeList.
const attrListAttrID uint32 = 0xFFFB

// globalAttrNames maps the standard Matter global attribute IDs (present on
// every cluster, per the spec "Global Attributes" table) to their display names.
var globalAttrNames = map[uint32]string{
	0xFFFD: "ClusterRevision",
	0xFFFC: "FeatureMap",
	0xFFFB: "AttributeList",
	0xFFFA: "EventList",
	0xFFF9: "AcceptedCommandList",
	0xFFF8: "GeneratedCommandList",
}

func init() {
	rootCmd.AddCommand(withGroup(newTreeCmd(), groupDevices))
}

func newTreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show device tree (endpoints, clusters, attributes)",
		Example: `  matter @1 tree
  matter @1 tree -L 1
  matter @kitchen tree -L 3
  matter @1 tree -L 4`,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID, _, err := requireTarget(cmd)
			if err != nil {
				return err
			}

			level, _ := cmd.Flags().GetInt("level")
			if level < 1 || level > 4 {
				return fmt.Errorf("--level must be between 1 and 4 (got %d)\n\n  1  endpoints only\n  2  endpoints + clusters (default)\n  3  endpoints + clusters + attribute names\n  4  endpoints + clusters + attribute names + values", level)
			}

			fabricID := viper.GetUint64("default-fabric-id")
			if fabricID == 0 {
				fabricID = 1
			}

			node, err := getNodeForCompletion(fabricID, nodeID)
			if err != nil {
				return fmt.Errorf("getting node %d: %w", nodeID, err)
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			w := cmd.OutOrStdout()

			data, err := buildTreeData(cmd.Context(), w, node, level, verbose)
			if err != nil {
				return err
			}

			outFile, _ := cmd.Flags().GetString("out")
			if outFile != "" {
				if err := output.RenderTreeSVG(data, outFile); err != nil {
					return err
				}
				fmt.Fprintf(w, "Tree rendered to %s\n", outFile)
				return nil
			}

			return output.FormatRichTree(w, data)
		},
	}
	cmd.Flags().IntP("level", "L", 2, "tree depth: 1=endpoints, 2=+clusters, 3=+attribute names, 4=+values")
	cmd.Flags().String("out", "", "render tree as SVG to file")
	return cmd
}

// buildTreeData collects all tree data up to the requested depth level and
// returns a TreeData structure ready for rendering. For levels 3 and 4 it
// establishes a CASE session (or uses the session daemon) to read attribute
// lists and, optionally, attribute values from the device.
func buildTreeData(ctx context.Context, w io.Writer, node *store.Node, level int, verbose bool) (*output.TreeData, error) {
	data := &output.TreeData{
		NodeID:      node.ID,
		NodeName:    node.Name,
		VendorID:    node.VendorID,
		ProductID:   node.ProductID,
		LastAddress: node.LastAddress,
		Level:       level,
	}

	// Populate basic structure from store data (always needed).
	for _, ep := range node.Endpoints {
		te := output.TreeEndpoint{
			ID:          ep.ID,
			DeviceTypes: ep.DeviceTypes,
		}
		for _, cl := range ep.Clusters {
			// Always prefer the registry name; fall back to the stored name
			// only if the registry doesn't know this cluster, and reject
			// hex-only fallbacks like "0x0033" from older store data.
			name := ""
			if info, ok := clusters.Global.ClusterByID(cl.ID); ok {
				name = info.DisplayName
			}
			if name == "" && len(cl.Name) > 0 && !(len(cl.Name) > 2 && cl.Name[:2] == "0x") {
				name = cl.Name
			}
			te.Clusters = append(te.Clusters, output.TreeCluster{
				ID:   cl.ID,
				Name: name,
			})
		}
		data.Endpoints = append(data.Endpoints, te)
	}

	if level <= 2 {
		return data, nil
	}

	// Level 3/4: augment with live attribute data from the device.
	stepper := output.NewStepper(w, verbose)

	nodeLabel := node.Name
	if nodeLabel == "" {
		nodeLabel = fmt.Sprintf("node %d", node.ID)
	}
	boldLabel := output.Bold(nodeLabel)

	// Step 1: establish connection.
	stepper.Step(fmt.Sprintf("Connecting to %s", boldLabel))
	dc, client, session, cleanup, err := treeEstablishConnection(ctx, node.ID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Connection failed: %v", err))
		return nil, fmt.Errorf("connecting to node %d: %w", node.ID, err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Step 2: read all attribute data (auto-completes step 1 with ✓).
	stepper.Step(fmt.Sprintf("Reading device information from %s", boldLabel))

	for ei := range data.Endpoints {
		ep := &data.Endpoints[ei]
		for ci := range ep.Clusters {
			cl := &ep.Clusters[ci]

			listCtx, listCancel := context.WithTimeout(ctx, 10*time.Second)
			attrIDs, listErr := treeReadAttrList(listCtx, dc, client, session, ep.ID, cl.ID)
			listCancel()

			if listErr != nil {
				cl.ListErr = treeFormatErr(listErr)
				continue
			}

			for _, attrID := range attrIDs {
				cl.Attrs = append(cl.Attrs, output.TreeAttribute{
					ID:   attrID,
					Name: treeResolveAttrName(cl.ID, attrID),
				})
			}

			if level >= 4 {
				for ai := range cl.Attrs {
					attr := &cl.Attrs[ai]
					valCtx, valCancel := context.WithTimeout(ctx, 5*time.Second)
					value, valErr := treeReadAttrValue(valCtx, dc, client, session, ep.ID, cl.ID, attr.ID)
					valCancel()

					if valErr != nil {
						attr.Err = treeFormatErr(valErr)
					} else {
						attr.Value = value
					}
				}
			}
		}
	}

	// Complete step 2 with ✓ and leave the cursor on a clean line.
	stepper.Clear()
	return data, nil
}

// treeEstablishConnection returns either a daemon connection or a direct CASE
// session. Exactly one of dc or (client, session) is non-nil on success.
func treeEstablishConnection(ctx context.Context, nodeID uint64) (
	dc *daemonNodeConn,
	client *interaction.Client,
	session *protocol.Session,
	cleanup func(),
	err error,
) {
	if d, ok := connectViaDaemon(nodeID); ok {
		return d, nil, nil, nil, nil
	}
	c, s, cl, e := connectToNode(ctx, nodeID)
	return nil, c, s, cl, e
}

// treeReadAttrRaw reads the raw TLV bytes for a single attribute. It uses the
// daemon when dc is non-nil, otherwise uses the direct CASE session.
func treeReadAttrRaw(ctx context.Context, dc *daemonNodeConn, client *interaction.Client, session *protocol.Session, ep uint16, clID, attrID uint32) ([]byte, error) {
	if dc != nil {
		dresp, err := dc.Read(daemon.AttrPathReq{
			Endpoint:    ep,
			ClusterID:   clID,
			AttributeID: attrID,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range dresp.Reports {
			if r.StatusCode != 0 {
				return nil, fmt.Errorf("status 0x%02X", r.StatusCode)
			}
			raw, _ := daemon.DecodeFields(r.Data)
			return raw, nil
		}
		return nil, nil
	}

	// Direct CASE session.
	path := interaction.NewAttributePath(ep, clID, attrID)
	reports, err := client.Read(ctx, session, path)
	if err != nil {
		return nil, err
	}
	for _, r := range reports {
		if r.Status != nil {
			return nil, fmt.Errorf("status 0x%02X", r.Status.Status.Status)
		}
		if r.Data != nil {
			return r.Data.Data, nil
		}
	}
	return nil, nil
}

// treeReadAttrList reads the AttributeList (0xFFFB) for a cluster and returns
// the attribute IDs it contains.
func treeReadAttrList(ctx context.Context, dc *daemonNodeConn, client *interaction.Client, session *protocol.Session, ep uint16, clID uint32) ([]uint32, error) {
	raw, err := treeReadAttrRaw(ctx, dc, client, session, ep, clID, attrListAttrID)
	if err != nil {
		return nil, err
	}
	return treeDecodeAttrList(raw), nil
}

// treeDecodeAttrList parses the raw TLV of an AttributeList value and returns
// the list of attribute IDs. Returns nil if the data cannot be decoded.
func treeDecodeAttrList(raw []byte) []uint32 {
	if len(raw) == 0 {
		return nil
	}
	r := tlv.NewReader(bytes.NewReader(raw))

	// The outer element should be a List or Array container.
	if err := r.Next(); err != nil {
		return nil
	}
	if t := r.Type(); t != tlv.TypeList && t != tlv.TypeArray {
		return nil
	}

	var ids []uint32
	for {
		if err := r.Next(); err != nil {
			break
		}
		if r.Type() == tlv.TypeEndOfContainer {
			break
		}
		if v, ok := r.Value().(uint64); ok {
			ids = append(ids, uint32(v))
		}
	}
	return ids
}

// treeReadAttrValue reads a single attribute and returns its formatted string value.
func treeReadAttrValue(ctx context.Context, dc *daemonNodeConn, client *interaction.Client, session *protocol.Session, ep uint16, clID, attrID uint32) (string, error) {
	raw, err := treeReadAttrRaw(ctx, dc, client, session, ep, clID, attrID)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "<no data>", nil
	}
	return decodeTLVValue(raw), nil
}

// treeResolveAttrName looks up the display name for an attribute ID within a
// cluster. It checks:
//  1. The cluster-specific attribute list in the registry.
//  2. The global attribute table (0xFFF8–0xFFFD, present on every cluster).
//  3. Falls back to a hex representation for truly unknown attributes.
func treeResolveAttrName(clusterID, attrID uint32) string {
	if cl, ok := clusters.Global.ClusterByID(clusterID); ok {
		for _, a := range cl.Attributes {
			if a.ID == attrID {
				if a.DisplayName != "" {
					return a.DisplayName
				}
				return a.Name
			}
		}
	}
	if name, ok := globalAttrNames[attrID]; ok {
		return name
	}
	return fmt.Sprintf("0x%04X", attrID)
}

// treeFormatErr converts a read error into a compact inline string.
func treeFormatErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "deadline exceeded") || strings.Contains(s, "timeout") {
		return "<timeout>"
	}
	// Matter UnsupportedAccess = 0x7E
	if strings.Contains(s, "0x7E") || strings.Contains(s, "access denied") ||
		strings.Contains(s, "UnsupportedAccess") {
		return "<access denied>"
	}
	return fmt.Sprintf("<error: %v>", err)
}
