// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package completion provides dynamic shell completion helpers for matter-cli.
// It uses the cluster registry and store to generate context-aware completions
// for cluster names, attribute names, command names, node IDs, and @target
// tokens.
package completion

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/daemon"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// completionTimeout is the maximum time to wait for the database lock when
// the daemon is not running (so no contention is expected). Only used as a
// fallback; when the daemon is running we query it via its Unix socket instead.
const completionTimeout = 100 * time.Millisecond

// loadNodes returns all commissioned nodes for use in completion functions.
// When the session daemon is running it queries the daemon via its Unix socket
// so that the BoltDB file lock held by the daemon is not contended. When no
// daemon is running, it opens the database directly with a short timeout.
func loadNodes(fabricID uint64) ([]*store.Node, error) {
	dc := daemon.NewClient("")
	if dc.IsRunning() {
		return dc.ListNodes(fabricID)
	}
	dbPath, err := store.DefaultDBPath()
	if err != nil {
		return nil, err
	}
	s, err := store.NewBoltStoreTimeout(dbPath, completionTimeout)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.ListNodes(fabricID)
}

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

// FieldFlagCompletions returns completions for command fields. When toComplete
// contains "=" (e.g. "Direction="), it returns enum values for that field.
// Otherwise it returns "FieldName=" completions for all unset fields.
func FieldFlagCompletions(fields []clusters.CommandFieldInfo, existing []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// If the user is typing a value (e.g. "Direction="), show enum values.
	if name, _, ok := strings.Cut(toComplete, "="); ok {
		for _, f := range fields {
			if !strings.EqualFold(f.Name, name) || len(f.EnumValues) == 0 {
				continue
			}
			completions := make([]string, len(f.EnumValues))
			for i, ev := range f.EnumValues {
				completions[i] = fmt.Sprintf("%s=%s\t%d", f.Name, ev.Name, ev.Value)
			}
			return completions, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Show FieldName= completions for unset fields.
	provided := make(map[string]bool)
	for _, kv := range existing {
		if n, _, ok := strings.Cut(kv, "="); ok {
			provided[strings.TrimSpace(n)] = true
		}
	}
	var completions []string
	for _, f := range fields {
		if provided[f.Name] {
			continue
		}
		status := "required"
		if f.Optional {
			status = "optional"
		}
		completions = append(completions, fmt.Sprintf("%s=\t%s · %s", f.Name, f.Type, status))
	}
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
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

	// Well-known Matter device type IDs from the Matter Device Library spec.
	names := map[uint32]string{
		// Infrastructure
		0x0016: "Root Node",
		0x0019: "Bridged Node",
		0x000E: "Aggregator",
		0x000F: "Power Source",
		// Lighting
		0x0100: "On/Off Light",
		0x0101: "Dimmable Light",
		0x0102: "Color Temperature Light",
		0x0103: "Extended Color Light",
		// Plugs & outlets
		0x0104: "On/Off Plug-in Unit",
		0x0105: "Dimmable Plug-in Unit",
		// Switches & controls
		0x010A: "On/Off Light Switch",
		0x010B: "Dimmer Switch",
		0x010C: "Color Dimmer Switch",
		0x010D: "Generic Switch",
		0x0840: "Control Bridge",
		0x0050: "On/Off Sensor",
		// Window coverings
		0x0202: "Window Covering",
		0x0203: "Window Covering Controller",
		// HVAC & fans
		0x0300: "Heating/Cooling Unit",
		0x0301: "Thermostat",
		0x002B: "Fan",
		// Air quality (Matter 1.2+)
		0x002C: "Air Quality Sensor",
		0x002D: "Air Purifier",
		// Access control
		0x000A: "Door Lock",
		0x000B: "Door Lock Controller",
		// Sensors
		0x0015: "Contact Sensor",
		0x0106: "Light Sensor",
		0x0107: "Occupancy Sensor",
		0x0302: "Temperature Sensor",
		0x0303: "Pump",
		0x0304: "Pump Controller",
		0x0305: "Pressure Sensor",
		0x0306: "Flow Sensor",
		0x0307: "Humidity Sensor",
		// Appliances
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
		fabricID := viper.GetUint64("default-fabric-id")
		if fabricID == 0 {
			fabricID = 1
		}

		nodes, err := loadNodes(fabricID)
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

// RootCompletionFunc returns a cobra ValidArgsFunction for the root command
// that handles two completion types:
//
//   - @target tokens (e.g. "@1/2") — delegated to TargetCompletionFunc.
//   - cluster shorthand commands — case-insensitive prefix/substring match of
//     cluster names, so typing "on<TAB>" offers "OnOff" and "level<TAB>" offers
//     "LevelControl".
//
// The cluster name completions are offered only when toComplete does not start
// with "@", and use the same search infrastructure as ClusterNameCompletion.
//
// allowedClusters, if non-nil, is called at completion time to filter results
// to the set of cluster IDs present on the current target endpoint. A nil map
// means no filter (show all clusters); a non-nil but empty map means no
// clusters are applicable (e.g. node-only target without an endpoint).
func RootCompletionFunc(
	registry *clusters.Registry,
	allowedClusters func() map[uint32]bool,
) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	targetFn := TargetCompletionFunc()
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if strings.HasPrefix(toComplete, "@") {
			return targetFn(cmd, args, toComplete)
		}
		results := registry.SearchClusters(toComplete)

		var allowed map[uint32]bool
		if allowedClusters != nil {
			allowed = allowedClusters()
		}
		if allowed != nil {
			kept := results[:0]
			for _, c := range results {
				if allowed[c.ID] {
					kept = append(kept, c)
				}
			}
			results = kept
		}

		if len(results) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := make([]string, len(results))
		for i, c := range results {
			names[i] = fmt.Sprintf("%s\t%s (0x%04X)", c.Name, c.DisplayName, c.ID)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}

// TargetCompletionFunc returns a cobra ValidArgsFunction that completes
// @target tokens in two stages. Emitted tokens are always numeric @N;
// device names are shown only in the description as a visual hint.
//
// Stage 1 — node-only (no "/" typed yet):
// When the user types "@" or "@ki", completions are numeric node tokens
// (e.g. "@1", "@2"). Prefix matching considers both the numeric ID and
// the device's kebab-case name, so "@ki" can narrow down to a node named
// "Kitchen Light". ShellCompDirectiveNoSpace is included so the shell
// does not append a trailing space, allowing the user to either type
// "/" to proceed to endpoint selection or " " (space) for device
// commands.
//
// Stage 2 — endpoint selection ("/" present):
// When the user types "@1/", completions are the non-root endpoints on
// the matched node (e.g. "@1/1", "@1/2"). Normal trailing space is
// applied so the user can proceed to a command after selection.
func TargetCompletionFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		// Only complete if the user is typing a @target token.
		if !strings.HasPrefix(toComplete, "@") {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		fabricID := viper.GetUint64("default-fabric-id")
		if fabricID == 0 {
			fabricID = 1
		}

		nodes, err := loadNodes(fabricID)
		if err != nil || len(nodes) == 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		// Sort nodes by ID so completions are presented in a stable,
		// predictable order regardless of commissioning sequence.
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

		// Strip "@" and split into name-part and optional endpoint-part.
		partial := strings.ToLower(toComplete[1:])
		namePart, epPart, hasSlash := strings.Cut(partial, "/")

		var completions []string
		for _, n := range nodes {
			idStr := fmt.Sprintf("%d", n.ID)

			// Human-readable alias: kebab-case name when available, numeric otherwise.
			alias := idStr
			if n.Name != "" {
				alias = strings.ReplaceAll(strings.ToLower(n.Name), " ", "-")
			}

			// Filter by the name/ID part the user has typed so far.
			nameMatches := namePart == "" ||
				strings.HasPrefix(alias, namePart) ||
				strings.HasPrefix(idStr, namePart)
			if !nameMatches {
				continue
			}

			if !hasSlash {
				// ── Stage 1: node-only completions ──
				//
				// Always emit the numeric @N token. The alias is shown in the
				// description (with [nodeID] appended) so each entry is unique
				// and zsh renders them as a vertical list rather than grouping
				// identical descriptions into a single row.
				var desc string
				if alias != idStr {
					desc = fmt.Sprintf("%s [%s]  %s", alias, idStr, nodeSummary(n))
				} else {
					desc = fmt.Sprintf("[%s]  %s", idStr, nodeSummary(n))
				}
				completions = append(completions, fmt.Sprintf("@%s\t%s", idStr, desc))
			} else {
				// ── Stage 2: endpoint completions for the matched node ──
				//
				// Only complete when the user typed a numeric node prefix (e.g.
				// "@1/"). Name-based prefixes (e.g. "@kitchen-light/") are
				// never emitted as tokens; the shell would discard
				// completions that do not share the typed prefix anyway.
				if !isAllDigits(namePart) {
					continue
				}

				for _, ep := range n.Endpoints {
					epStr := fmt.Sprintf("%d", ep.ID)

					// Filter by endpoint prefix the user has typed after "/".
					if !strings.HasPrefix(epStr, epPart) {
						continue
					}

					epDesc := endpointDescription(ep)
					var desc string
					if alias != idStr {
						desc = fmt.Sprintf("%s  [%s]", epDesc, alias)
					} else {
						desc = epDesc
					}
					completions = append(completions, fmt.Sprintf("@%s/%s\t%s", idStr, epStr, desc))
				}
			}
		}

		if !hasSlash {
			// Node-only stage: suppress trailing space so the user can type
			// "/" for endpoint or " " for device-level commands.
			return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// isAllDigits reports whether s is non-empty and consists only of ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// nodeSummary returns a one-line human-readable description of a node,
// using the device type of its first non-root endpoint when available.
func nodeSummary(n *store.Node) string {
	for _, ep := range n.Endpoints {
		if ep.ID > 0 {
			return endpointDescription(ep)
		}
	}
	if n.Name != "" {
		return n.Name
	}
	return fmt.Sprintf("Node %d", n.ID)
}
