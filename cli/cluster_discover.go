// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/p0fi/matter-cli/cli/completion"
	"github.com/p0fi/matter-cli/cli/output"
	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// discoverAttrListTimeout bounds a single AttributeList read. It matches the
// per-cluster budget `tree -L 3` uses so a wedged cluster cannot stall a
// node-wide walk.
const discoverAttrListTimeout = 10 * time.Second

// DiscoveredCluster is one cluster instance visited by `cluster discover`,
// as reported in the command's json/yaml output.
type DiscoveredCluster struct {
	Endpoint   uint16   `json:"endpoint"`
	ClusterID  uint32   `json:"cluster_id"`
	Cluster    string   `json:"cluster"`
	Attributes []uint32 `json:"attributes,omitempty"`
	// Error is the failure reason when the AttributeList read did not
	// succeed. The cluster's previously cached list is left untouched in that
	// case, so a transient failure never discards known-good data.
	Error string `json:"error,omitempty"`
}

// newClusterDiscoverCmd creates the `matter cluster discover` subcommand, which
// reads each cluster's AttributeList (0xFFFB) from the device and caches the
// result so shell completion can offer only the attributes that cluster
// instance actually implements.
func newClusterDiscoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Read and cache the attributes each cluster advertises",
		Long: `Read the AttributeList (0xFFFB) of every cluster on the target and cache the
result locally.

Shell completion for read, write, and subscribe uses that cache to offer only
the attributes a device actually implements, instead of every attribute the
Matter spec defines for the cluster type. Re-run this command whenever a
device's firmware changes to refresh the cache.

A node-only target (@1) walks every endpoint; an endpoint-explicit target
(@1/1) is scoped to that endpoint. Either scope can be narrowed to a single
cluster with --cluster.

Clusters whose AttributeList read fails keep their previously cached list: a
stale-but-once-verified list still beats no filtering at all.`,
		Example: `  matter @1 cluster discover
  matter @1/1 cluster discover
  matter @1 cluster discover --cluster OnOff`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClusterDiscover(cmd)
		},
	}
	cmd.Flags().StringP("cluster", "C", "", "limit discovery to a single cluster (name or ID)")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completion.ClusterNameCompletion(clusters.Global))
	return cmd
}

// runClusterDiscover performs the AttributeList sweep and persists the result.
func runClusterDiscover(cmd *cobra.Command) error {
	if resolvedTarget == nil || resolvedTarget.NodeID == 0 {
		return errors.New(noTargetError)
	}
	// Unlike requireTarget, this reads ExplicitEndpoint directly: requireTarget
	// collapses "no endpoint given" into endpoint 0, which would make `@1`
	// (walk every endpoint) indistinguishable from `@1/0` (endpoint 0 only).
	target := resolvedTarget

	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	node, err := loadNodeForCompletion(fabricID, target.NodeID)
	if err != nil {
		return fmt.Errorf("getting node %d: %w", target.NodeID, err)
	}

	var only *clusters.ClusterInfo
	if name, _ := cmd.Flags().GetString("cluster"); name != "" {
		cl, ok := clusters.Global.ClusterByName(name)
		if !ok {
			return fmt.Errorf("unknown cluster %q", name)
		}
		only = cl
	}

	targets := discoverTargets(node, target, only)
	if len(targets) == 0 {
		return discoverNoTargetsError(node, target, only)
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	w := cmd.OutOrStdout()
	stepper := output.NewStepper(w, verbose)

	results, updated, failed, err := sweepAttributeLists(cmd, stepper, node, targets)
	if err != nil {
		return err
	}

	// Persist only after the sweep has released its connection: a direct CASE
	// session holds the BoltDB lock for its whole lifetime (see
	// docs/DAEMON_STORE.md), and persistNode would block on that lock forever.
	if updated > 0 {
		if err := persistNode(fabricID, node); err != nil {
			return fmt.Errorf("saving attribute lists for node %d: %w", node.ID, err)
		}
	}

	format, _ := cmd.Flags().GetString("format")
	f := output.New(format)
	if _, ok := f.(*output.TableFormatter); !ok {
		return f.Format(w, results)
	}
	return renderDiscoverTable(w, f, results, updated, failed)
}

// sweepAttributeLists connects to the node, reads every target's AttributeList,
// and releases the connection before returning.
//
// The scoping is load-bearing: connectToNode keeps the BoltDB handle open for
// the life of a direct CASE session, so the caller must not try to persist
// anything until cleanup has run.
func sweepAttributeLists(
	cmd *cobra.Command,
	stepper *output.Stepper,
	node *store.Node,
	targets []discoverTarget,
) (results []DiscoveredCluster, updated, failed int, err error) {
	label := output.Bold(resolveNodeLabel(node.ID))

	stepper.Step(fmt.Sprintf("Connecting to %s", label))
	dc, client, session, cleanup, err := treeEstablishConnection(cmd.Context(), node.ID)
	if err != nil {
		stepper.Fail(fmt.Sprintf("Connection failed: %v", err))
		return nil, 0, 0, fmt.Errorf("connecting to node %d: %w", node.ID, err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	stepper.Step(fmt.Sprintf("Reading attribute lists from %s", label))

	read := func(ctx context.Context, endpoint uint16, clusterID uint32) ([]uint32, error) {
		listCtx, cancel := context.WithTimeout(ctx, discoverAttrListTimeout)
		defer cancel()
		return treeReadAttrList(listCtx, dc, client, session, endpoint, clusterID)
	}

	results, updated, failed = discoverAttributeLists(cmd.Context(), node, targets, read)
	stepper.Clear()
	return results, updated, failed, nil
}

// discoverAttributeLists reads the AttributeList// discoverAttributeLists reads the AttributeList of every target and caches each
// successful result on node. It returns one result per target, plus the counts
// of clusters whose cache was refreshed and whose read failed.
//
// A failed read is reported but never applied, so the cluster keeps whatever
// list was cached before — see recordAttrListResult.
func discoverAttributeLists(
	ctx context.Context,
	node *store.Node,
	targets []discoverTarget,
	read attrListReader,
) (results []DiscoveredCluster, updated, failed int) {
	results = make([]DiscoveredCluster, 0, len(targets))
	for _, t := range targets {
		res := DiscoveredCluster{
			Endpoint:  t.endpoint,
			ClusterID: t.clusterID,
			Cluster:   discoverClusterLabel(t.clusterID, t.clusterName),
		}

		attrIDs, err := read(ctx, t.endpoint, t.clusterID)
		if err != nil {
			res.Error = treeFormatErr(err)
			failed++
		} else {
			res.Attributes = attrIDs
			recordAttrListResult(node, t.endpoint, t.clusterID, attrIDs, nil)
			updated++
		}
		results = append(results, res)
	}
	return results, updated, failed
}

// renderDiscoverTable prints the per-cluster results plus a one-line summary
// telling the user what the refreshed cache now enables.
func renderDiscoverTable(w io.Writer, f output.Formatter, results []DiscoveredCluster, updated, failed int) error {
	td := &output.TableData{Headers: []string{"ENDPOINT", "CLUSTER", "ATTRIBUTES"}}
	for _, r := range results {
		attrs := fmt.Sprintf("%d", len(r.Attributes))
		if r.Error != "" {
			attrs = r.Error
		}
		td.Rows = append(td.Rows, []string{
			fmt.Sprintf("%d", r.Endpoint),
			r.Cluster,
			attrs,
		})
	}
	if err := f.Format(w, td); err != nil {
		return err
	}

	summary := fmt.Sprintf("Cached attribute lists for %s.",
		output.Bold(pluralClusters(updated)))
	if failed > 0 {
		summary += fmt.Sprintf(" %s %s could not be read; their previous cache was kept.",
			output.WarningIcon(), pluralClusters(failed))
	}
	fmt.Fprintf(w, "\n%s\n", summary)
	return nil
}

// pluralClusters renders a cluster count with the right noun.
func pluralClusters(n int) string {
	if n == 1 {
		return "1 cluster"
	}
	return fmt.Sprintf("%d clusters", n)
}

// discoverTarget is a single endpoint/cluster pair to read an AttributeList from.
type discoverTarget struct {
	endpoint    uint16
	clusterID   uint32
	clusterName string
}

// discoverTargets expands a resolved target into the endpoint/cluster pairs to
// visit. A node-only target covers every endpoint; an endpoint-explicit target
// covers just that endpoint. only, when non-nil, narrows either scope to one
// cluster. Client-side cluster references are skipped — an AttributeList read
// only makes sense against a server cluster.
func discoverTargets(node *store.Node, target *Target, only *clusters.ClusterInfo) []discoverTarget {
	var out []discoverTarget
	for _, ep := range node.Endpoints {
		if target.ExplicitEndpoint && ep.ID != target.Endpoint {
			continue
		}
		for _, cl := range ep.Clusters {
			if cl.Side != "server" && cl.Side != "" {
				continue
			}
			if only != nil && cl.ID != only.ID {
				continue
			}
			out = append(out, discoverTarget{
				endpoint:    ep.ID,
				clusterID:   cl.ID,
				clusterName: cl.Name,
			})
		}
	}
	return out
}

// discoverNoTargetsError explains an empty sweep in terms of whichever part of
// the target did not match, so the user knows which one to correct.
func discoverNoTargetsError(node *store.Node, target *Target, only *clusters.ClusterInfo) error {
	switch {
	case only != nil && target.ExplicitEndpoint:
		return fmt.Errorf("node %d has no %s cluster on endpoint %d", node.ID, only.Name, target.Endpoint)
	case only != nil:
		return fmt.Errorf("node %d has no %s cluster on any endpoint", node.ID, only.Name)
	case target.ExplicitEndpoint:
		return fmt.Errorf("node %d has no server clusters on endpoint %d", node.ID, target.Endpoint)
	default:
		return fmt.Errorf("node %d has no server clusters; run `matter @%d tree` to inspect it", node.ID, node.ID)
	}
}

// discoverClusterLabel prefers the registry's display name for a cluster,
// falling back to the stored name and finally to a hex ID.
func discoverClusterLabel(clusterID uint32, stored string) string {
	if info, ok := clusters.Global.ClusterByID(clusterID); ok {
		return info.DisplayName
	}
	if stored != "" {
		return stored
	}
	return fmt.Sprintf("0x%04X", clusterID)
}
