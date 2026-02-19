// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package completion provides dynamic shell completion helpers for matter-cli.
// It uses the cluster registry and store to generate context-aware completions
// for cluster names, attribute names, command names, node IDs, and @target
// tokens.
package completion

import (
	"fmt"
	"strings"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ClusterNameCompletion returns a cobra ValidArgsFunction that completes
// cluster names from the global cluster registry.
func ClusterNameCompletion(registry *clusters.Registry) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		results := registry.SearchClusters(toComplete)
		names := make([]string, len(results))
		for i, c := range results {
			names[i] = fmt.Sprintf("%s\t%s (0x%04X)", c.Name, c.DisplayName, c.ID)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// AttributeNameCompletion returns a cobra ValidArgsFunction that completes
// attribute names for the cluster specified by the --cluster flag.
func AttributeNameCompletion(registry *clusters.Registry) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		cluster, ok := registry.ClusterByName(clusterName)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}
		results := registry.SearchAttributes(cluster.ID, toComplete)
		names := make([]string, len(results))
		for i, a := range results {
			names[i] = fmt.Sprintf("%s\t%s (0x%04X)", a.Name, a.DisplayName, a.ID)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// CommandNameCompletion returns a cobra ValidArgsFunction that completes
// command names for the cluster specified by the --cluster flag.
func CommandNameCompletion(registry *clusters.Registry) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		cluster, ok := registry.ClusterByName(clusterName)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, c := range cluster.Commands {
			names = append(names, fmt.Sprintf("%s\t%s (0x%04X)", c.Name, c.DisplayName, c.ID))
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// NodeIDCompletion returns a cobra ValidArgsFunction that completes node IDs
// from the given store using the specified fabric ID.
func NodeIDCompletion(s store.Store, fabricID uint64) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		nodes, err := s.ListNodes(fabricID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		names := make([]string, len(nodes))
		for i, n := range nodes {
			names[i] = fmt.Sprintf("%d\t%s", n.ID, n.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// EndpointIDCompletionFunc returns a cobra completion function that lazily
// opens the store at completion time, reads the --node flag from the command
// being completed, looks up that node, and returns its endpoint IDs with
// human-friendly descriptions (device type and cluster summary).
//
// If the --node flag is not set or the node cannot be found, the function
// returns an empty list so the shell completion experience is not disrupted.
func EndpointIDCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		nodeID, err := cmd.Flags().GetUint64("node")
		if err != nil || nodeID == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		dbPath, err := store.DefaultDBPath()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		s, err := store.NewBoltStore(dbPath)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		defer s.Close()

		fabricID := viper.GetUint64("default-fabric-id")
		if fabricID == 0 {
			fabricID = 1
		}

		node, err := s.GetNode(fabricID, nodeID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for _, ep := range node.Endpoints {
			epStr := fmt.Sprintf("%d", ep.ID)
			if toComplete != "" && !strings.HasPrefix(epStr, toComplete) {
				continue
			}

			// Build a description from device type and cluster names.
			desc := endpointDescription(ep)
			completions = append(completions, fmt.Sprintf("%s\t%s", epStr, desc))
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// endpointDescription builds a human-friendly description for an endpoint,
// using its device type name (if known) and a summary of its server clusters.
func endpointDescription(ep store.Endpoint) string {
	dtName := deviceTypeName(ep)
	if dtName != "" {
		return dtName
	}

	// Fall back to listing cluster names.
	if len(ep.Clusters) > 0 {
		var names []string
		for _, cl := range ep.Clusters {
			if cl.Name != "" {
				names = append(names, cl.Name)
			}
		}
		if len(names) > 3 {
			return fmt.Sprintf("%s +%d more", strings.Join(names[:3], ", "), len(names)-3)
		}
		if len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}

	return fmt.Sprintf("Endpoint %d", ep.ID)
}

// deviceTypeName returns a human-friendly name for well-known Matter device
// type IDs, or an empty string if the device type is not recognized.
func deviceTypeName(ep store.Endpoint) string {
	if len(ep.DeviceTypes) == 0 {
		return ""
	}
	id := ep.DeviceTypes[0].ID

	// Well-known Matter device type IDs from the Matter Application Cluster spec.
	names := map[uint32]string{
		0x0016: "Root Node",
		0x0019: "Bridged Node",
		0x000E: "Aggregator",
		0x000F: "Power Source",
		0x0100: "On/Off Light",
		0x0101: "Dimmable Light",
		0x0102: "Color Temperature Light",
		0x0103: "Extended Color Light",
		0x0104: "On/Off Plug-in Unit",
		0x0105: "Dimmable Plug-in Unit",
		0x010A: "On/Off Light Switch",
		0x010B: "Dimmer Switch",
		0x010C: "Color Dimmer Switch",
		0x010D: "Generic Switch",
		0x0202: "Window Covering",
		0x0300: "Heating/Cooling Unit",
		0x0301: "Thermostat",
		0x002B: "Fan",
		0x0303: "Air Purifier",
		0x000A: "Door Lock",
		0x000B: "Door Lock Controller",
		0x0015: "Contact Sensor",
		0x0106: "Light Sensor",
		0x0107: "Occupancy Sensor",
		0x0302: "Temperature Sensor",
		0x0305: "Pressure Sensor",
		0x0306: "Flow Sensor",
		0x0307: "Humidity Sensor",
		0x0044: "Air Quality Sensor",
		0x0070: "Refrigerator",
		0x0071: "Temperature Controlled Cabinet",
		0x0072: "Room Air Conditioner",
		0x0073: "Laundry Washer",
		0x0074: "Robotic Vacuum Cleaner",
		0x0075: "Dishwasher",
		0x0076: "Smoke/CO Alarm",
	}

	if name, ok := names[id]; ok {
		return name
	}
	return fmt.Sprintf("Device 0x%04X", id)
}

// NodeIDCompletionFunc returns a cobra completion function that lazily opens
// the store at completion time, reads the fabric ID from viper config, and
// returns all known node IDs with their human-friendly names as completion
// candidates. The store is opened and closed on each completion invocation so
// that no persistent handle is required at flag-registration time.
//
// If the store cannot be opened (e.g. first run, no devices commissioned yet),
// the function gracefully returns an empty list instead of an error so the
// shell completion experience is not disrupted.
func NodeIDCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		dbPath, err := store.DefaultDBPath()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		s, err := store.NewBoltStore(dbPath)
		if err != nil {
			// Store doesn't exist yet — no nodes to complete.
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		defer s.Close()

		fabricID := viper.GetUint64("default-fabric-id")
		if fabricID == 0 {
			fabricID = 1
		}

		nodes, err := s.ListNodes(fabricID)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for _, n := range nodes {
			entry := fmt.Sprintf("%d\t%s", n.ID, n.Name)
			// If the user has started typing, filter to matching prefixes.
			if toComplete == "" || strings.HasPrefix(fmt.Sprintf("%d", n.ID), toComplete) {
				completions = append(completions, entry)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// TargetCompletionFunc returns a cobra ValidArgsFunction that completes
// @target tokens. When the user types "@" and presses Tab, this returns
// all known nodes formatted as @nodeID/endpoint suggestions. Both numeric
// IDs and device aliases (friendly names) are offered.
//
// Examples of completions produced:
//
//	@1/1    Kitchen Light (endpoint 1)
//	@2/1    Front Door Lock (endpoint 1)
//	@kitchen/1  Kitchen Light (node 1)
//
// This function is intended to be registered on the root command's
// ValidArgsFunction so that @target can be completed at any position.
func TargetCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Only complete if the user is typing a @target token.
		if !strings.HasPrefix(toComplete, "@") {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		dbPath, err := store.DefaultDBPath()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		s, err := store.NewBoltStore(dbPath)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		defer s.Close()

		fabricID := viper.GetUint64("default-fabric-id")
		if fabricID == 0 {
			fabricID = 1
		}

		nodes, err := s.ListNodes(fabricID)
		if err != nil || len(nodes) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Strip the "@" prefix from what the user typed so far for matching.
		partial := toComplete[1:]

		var completions []string
		for _, n := range nodes {
			nodeStr := fmt.Sprintf("%d", n.ID)

			// Find the first non-root endpoint to use as default.
			var defaultEP uint16 = 1
			for _, ep := range n.Endpoints {
				if ep.ID > 0 {
					defaultEP = ep.ID
					break
				}
			}

			// Offer @nodeID and @nodeID/endpoint completions.
			idTarget := fmt.Sprintf("@%s/%d", nodeStr, defaultEP)
			idDesc := n.Name
			if idDesc == "" {
				idDesc = fmt.Sprintf("Node %s", nodeStr)
			}
			if partial == "" || strings.HasPrefix(nodeStr, partial) {
				completions = append(completions,
					fmt.Sprintf("%s\t%s", idTarget, idDesc))
			}

			// Also offer @alias completions if the node has a name.
			if n.Name != "" {
				aliasLower := strings.ToLower(n.Name)
				aliasKebab := strings.ReplaceAll(aliasLower, " ", "-")
				aliasTarget := fmt.Sprintf("@%s/%d", aliasKebab, defaultEP)
				if partial == "" || strings.HasPrefix(aliasKebab, strings.ToLower(partial)) {
					completions = append(completions,
						fmt.Sprintf("%s\t%s (node %s)", aliasTarget, n.Name, nodeStr))
				}
			}

			// If the user typed @nodeID/ already, offer per-endpoint completions.
			if strings.HasPrefix(partial, nodeStr+"/") {
				epPartial := partial[len(nodeStr)+1:]
				for _, ep := range n.Endpoints {
					epStr := fmt.Sprintf("%d", ep.ID)
					if epPartial == "" || strings.HasPrefix(epStr, epPartial) {
						epDesc := endpointDescription(ep)
						completions = append(completions,
							fmt.Sprintf("@%s/%s\t%s", nodeStr, epStr, epDesc))
					}
				}
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
}
