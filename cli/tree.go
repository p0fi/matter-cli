// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// Per-read budgets for the live reads that levels 3 and 4 perform, so one
// unresponsive cluster cannot stall the whole traversal.
const (
	treeAttrListTimeout = 10 * time.Second

	// treeClusterWildcardTimeout bounds a level-4 wildcard read of one
	// cluster's attributes. Unlike the old per-attribute loop — where one
	// slow attribute cost treeAttrValueTimeout and the rest still rendered —
	// a wildcard read is bounded as a whole, so a stalled cluster now loses
	// every attribute in it. It gets the same 30s budget `cluster read`
	// uses (clusterReadTimeout), since a wildcard read returns more data
	// than the AttributeList-only read treeAttrListTimeout was sized for.
	treeClusterWildcardTimeout = clusterReadTimeout
)

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
  matter @2 tree -L 3
  matter @1 tree -L 4`,
		RunE: func(cmd *cobra.Command, args []string) error {
			outFile, _ := cmd.Flags().GetString("out")
			if outFile != "" {
				resolved, err := expandUserTilde(outFile, runtime.GOOS, os.UserHomeDir)
				if err != nil {
					return fmt.Errorf("resolving --out path: %w", err)
				}
				outFile = resolved
			}

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

			node, err := loadNodeForCompletion(fabricID, nodeID)
			if err != nil {
				return fmt.Errorf("getting node %d: %w", nodeID, err)
			}

			verbose, _ := cmd.Flags().GetBool("verbose")
			w := cmd.OutOrStdout()

			data, cacheUpdated, err := buildTreeData(cmd.Context(), w, node, level, verbose)
			if err != nil {
				return err
			}

			// Levels 3 and 4 already read every cluster's AttributeList to
			// render the tree; write it through so attribute-name completion
			// gets the same scoping `matter cluster discover` provides, for
			// free. A save failure is not worth failing the tree over — the
			// user asked for output, not for a cache refresh.
			if cacheUpdated {
				if err := persistAttributeCache(fabricID, node); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s Could not cache attribute lists: %v\n",
						output.WarningIcon(), err)
				}
			}

			if outFile != "" {
				stepper := output.NewStepper(w, verbose)
				if err := output.RenderTreeSVG(data, outFile); err != nil {
					stepper.Fail(fmt.Sprintf("Failed to render SVG: %v", err))
					return err
				}
				stepper.Success(fmt.Sprintf("Tree rendered to %s", output.Value(outFile)))

				openFlag, _ := cmd.Flags().GetBool("open")
				if openFlag {
					if err := openFile(outFile); err != nil {
						stepper.Fail(fmt.Sprintf("Could not open file: %v", err))
					}
				}
				return nil
			}

			return output.FormatRichTree(w, data)
		},
	}
	cmd.Flags().IntP("level", "L", 2, "tree depth: 1=endpoints, 2=+clusters, 3=+attribute names, 4=+values")
	cmd.Flags().String("out", "", "render tree as SVG to file")
	cmd.Flags().Bool("open", false, "open the SVG file after rendering (requires --out)")
	return cmd
}

// openFile opens a file with the OS default application.
func openFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", abs)
	case "linux":
		cmd = exec.Command("xdg-open", abs)
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	return cmd.Run()
}

// buildTreeData collects all tree data up to the requested depth level and
// returns a TreeData structure ready for rendering. For levels 3 and 4 it
// establishes a CASE session (or uses the session daemon) to read attribute
// lists and, optionally, attribute values from the device.
//
// Every AttributeList it reads successfully is also written back into node's
// ClusterRef cache, which shell completion uses to scope attribute names to
// what the device implements. Clusters whose read failed keep whatever was
// cached before. The second return value reports whether any cache entry
// changed, so the caller knows whether persisting the node is worthwhile.
func buildTreeData(ctx context.Context, w io.Writer, node *store.Node, level int, verbose bool) (*output.TreeData, bool, error) {
	data := &output.TreeData{
		NodeID:               node.ID,
		NodeName:             node.Name,
		VendorID:             node.VendorID,
		ProductID:            node.ProductID,
		SpecificationVersion: node.SpecificationVersion,
		SoftwareVersion:      node.SoftwareVersion,
		SerialNumber:         node.SerialNumber,
		LastAddress:          node.LastAddress,
		Level:                level,
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
				Side: cl.Side,
			})
		}
		data.Endpoints = append(data.Endpoints, te)
	}

	if level <= 2 {
		return data, false, nil
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
		return nil, false, fmt.Errorf("connecting to node %d: %w", node.ID, err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	// Step 2: read all attribute data (auto-completes step 1 with ✓).
	stepper.Step(fmt.Sprintf("Reading device information from %s", boldLabel))

	readList := func(ctx context.Context, ep uint16, clID uint32) ([]uint32, error) {
		listCtx, cancel := context.WithTimeout(ctx, treeAttrListTimeout)
		defer cancel()
		return treeReadAttrList(listCtx, dc, client, session, ep, clID)
	}
	readCluster := func(ctx context.Context, ep uint16, clID uint32) ([]attrReport, error) {
		clCtx, cancel := context.WithTimeout(ctx, treeClusterWildcardTimeout)
		defer cancel()
		return treeReadClusterWildcard(clCtx, dc, client, session, ep, clID)
	}

	cacheUpdated := treePopulateAttributes(ctx, data, node, level, readList, readCluster)

	// Complete step 2 with ✓ and leave the cursor on a clean line.
	stepper.Clear()
	return data, cacheUpdated, nil
}

// treeClusterReader performs one wildcard read of every attribute a cluster
// instance reports, returning the reports in transport-neutral form.
type treeClusterReader func(ctx context.Context, endpoint uint16, clusterID uint32) ([]attrReport, error)

// treePopulateAttributes fills each cluster in data with the attribute names it
// advertises and, at level 4, their values. Level 3 uses the cheap AttributeList
// read since it only needs names; level 4 uses one wildcard read per cluster
// instead, since it needs every value anyway and a wildcard read returns them
// in the same round-trip AttributeList would have cost alone. Every
// AttributeList discovered — whether from the level-3 read or found among a
// level-4 wildcard read's reports — is also write-through into node's
// completion cache, so a tree run leaves attribute-name completion scoped
// exactly as `cluster discover` would. It reports whether any cache entry
// changed.
//
// The readers are injected so the traversal — including the partial-failure
// behaviour, where a cluster whose read failed keeps its previously cached
// list — is testable without a device.
func treePopulateAttributes(
	ctx context.Context,
	data *output.TreeData,
	node *store.Node,
	level int,
	readList attrListReader,
	readCluster treeClusterReader,
) bool {
	cacheUpdated := false

	for ei := range data.Endpoints {
		ep := &data.Endpoints[ei]
		for ci := range ep.Clusters {
			cl := &ep.Clusters[ci]

			if level == 4 {
				if treePopulateClusterWildcard(ctx, node, ep.ID, cl, readCluster) {
					cacheUpdated = true
				}
				continue
			}

			attrIDs, listErr := readList(ctx, ep.ID, cl.ID)
			if recordAttrListResult(node, ep.ID, cl.ID, attrIDs, listErr) {
				cacheUpdated = true
			}
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
		}
	}

	return cacheUpdated
}

// treePopulateClusterWildcard fills cl with the attributes a single wildcard
// read reports and write-throughs the AttributeList found among them into
// node's completion cache. A transport failure fails the whole cluster — it
// becomes cl.ListErr, exactly as a failed AttributeList read does at level 3 —
// while a per-attribute status inside a successful read stays scoped to that
// one attribute via its TreeAttribute.Err, same as before. It reports whether
// the cache changed.
func treePopulateClusterWildcard(ctx context.Context, node *store.Node, endpoint uint16, cl *output.TreeCluster, readCluster treeClusterReader) bool {
	reports, err := readCluster(ctx, endpoint, cl.ID)
	if err != nil {
		cl.ListErr = treeFormatErr(err)
		return false
	}

	cacheUpdated := false
	if attrIDs, ok := treeExtractAttributeList(reports); ok {
		cacheUpdated = recordAttrListResult(node, endpoint, cl.ID, attrIDs, nil)
	}

	clInfo := &clusters.ClusterInfo{ID: cl.ID, DisplayName: cl.Name}
	records := buildReadRecords(readTarget{nodeID: node.ID, endpoint: endpoint, cl: clInfo}, reports, time.Now(), fidelityCompact)
	cl.Attrs = make([]output.TreeAttribute, 0, len(records))
	for _, rec := range records {
		attr := output.TreeAttribute{ID: rec.AttributeID, Name: rec.Attribute}
		if rec.Error != "" {
			attr.Err = rec.Display
		} else {
			attr.Value = rec.Display
		}
		cl.Attrs = append(cl.Attrs, attr)
	}

	return cacheUpdated
}

// treeExtractAttributeList finds the AttributeList (0xFFFB) report among a
// wildcard read's reports and decodes it, reporting whether one was present
// and readable. A device that omits it, or answered it with a status instead
// of data, reports false — the caller must not write through in that case, so
// a partial or malformed response cannot wipe a previously cached list.
func treeExtractAttributeList(reports []attrReport) ([]uint32, bool) {
	for _, r := range reports {
		if r.attributeID != attrListAttrID {
			continue
		}
		if r.err != nil {
			return nil, false
		}
		return treeDecodeAttrList(r.data), true
	}
	return nil, false
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
		if len(dresp.Reports) == 0 {
			return nil, nil
		}
		r := dresp.Reports[0]
		if r.StatusCode != 0 {
			return nil, imStatusError(r.StatusCode, r.ClusterStatus)
		}
		raw, _ := daemon.DecodeFields(r.Data)
		return raw, nil
	}

	// Direct CASE session.
	path := interaction.NewAttributePath(ep, clID, attrID)
	reports, err := client.Read(ctx, session, path)
	if err != nil {
		return nil, err
	}
	for _, r := range reports {
		if r.Status != nil {
			return nil, imStatusError(r.Status.Status.Status, r.Status.Status.ClusterStatus)
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

// treeReadClusterWildcard reads every attribute of one cluster instance in a
// single wildcard ReadRequest, returning the reports in transport-neutral
// form. It uses the daemon when dc is non-nil, otherwise the direct CASE
// session.
func treeReadClusterWildcard(ctx context.Context, dc *daemonNodeConn, client *interaction.Client, session *protocol.Session, ep uint16, clID uint32) ([]attrReport, error) {
	if dc != nil {
		dresp, err := dc.Read(daemon.AttrPathReq{
			Endpoint:          ep,
			ClusterID:         clID,
			WildcardAttribute: true,
		})
		if err != nil {
			return nil, err
		}
		return daemonAttrReports(dresp.Reports), nil
	}

	path := interaction.NewWildcardAttributePath(ep, clID)
	reports, err := client.Read(ctx, session, path)
	if err != nil {
		return nil, err
	}
	return directAttrReports(reports), nil
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
