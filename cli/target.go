// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Target represents a resolved device target consisting of a node ID and an
// optional endpoint. When Endpoint is 0 and EndpointSet is false, the caller
// should treat the endpoint as unspecified (distinct from explicitly targeting
// endpoint 0).
type Target struct {
	NodeID      uint64
	Endpoint    uint16
	EndpointSet bool
	// ExplicitEndpoint is true when the endpoint was specified with a "/"
	// separator by the user (e.g. @node/1) or set explicitly via a sticky
	// default. It is false when the endpoint was inferred from an alias or
	// not specified at all. Only used for completion filtering — command
	// execution behaviour is unaffected.
	ExplicitEndpoint bool
}

// extractedTarget holds the target parsed from os.Args during Execute().
// It is set before cobra processes the args so that PersistentPreRunE can
// apply it via resolveTarget.
var extractedTarget *Target

// resolvedTarget is the final resolved target after the full resolution chain
// has been applied in resolveTarget. It is set during PersistentPreRunE and
// read by requireTarget.
var resolvedTarget *Target

// noTargetError is the error message shown when no target is specified and
// a command requires one. It lists all available methods for specifying a
// target so the user knows their options.
const noTargetError = `no target specified

Specify a device target using any of these methods:
  matter @1/1 on-off toggle          inline target (recommended)
  matter @kitchen on-off toggle      using a device alias
  matter use @1/1                    set a sticky default
  export MATTER_TARGET=@1/1          environment variable`

// ParseTarget parses a target string in the format "@node[/endpoint]".
// The node part can be a numeric ID or a device alias (resolved via the
// store). The endpoint part is optional and defaults to unset.
//
// Examples:
//
//	"@1"           → node 1, endpoint unset
//	"@1/2"         → node 1, endpoint 2
//	"@kitchen"     → alias "kitchen" resolved to a node ID
//	"@kitchen/1"   → alias "kitchen", endpoint 1
func ParseTarget(s string) (*Target, error) {
	if !strings.HasPrefix(s, "@") {
		return nil, fmt.Errorf("target must start with @")
	}
	raw := s[1:] // strip leading @
	if raw == "" {
		return nil, fmt.Errorf("empty target after @")
	}

	parts := strings.SplitN(raw, "/", 2)
	t := &Target{}

	// Parse the node part — try numeric first, then treat as alias.
	if id, err := strconv.ParseUint(parts[0], 10, 64); err == nil {
		if id == 0 {
			return nil, fmt.Errorf("node ID 0 is reserved")
		}
		t.NodeID = id
	} else {
		// Alias resolution — look up the name in the store.
		resolved, err := resolveAlias(parts[0])
		if err != nil {
			return nil, fmt.Errorf("resolving alias %q: %w", parts[0], err)
		}
		t.NodeID = resolved.NodeID
		// If the alias resolved with a default endpoint and no explicit
		// endpoint was given, inherit it.
		if resolved.EndpointSet && len(parts) == 1 {
			t.Endpoint = resolved.Endpoint
			t.EndpointSet = true
		}
	}

	// Parse the optional endpoint part.
	if len(parts) == 2 {
		ep, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid endpoint %q: %w", parts[1], err)
		}
		t.Endpoint = uint16(ep)
		t.EndpointSet = true
		t.ExplicitEndpoint = true
	}

	return t, nil
}

// resolveAlias looks up a device alias (friendly name) in the store and
// returns the matching target. The alias is matched case-insensitively
// against stored node names.
func resolveAlias(alias string) (*Target, error) {
	fabricID := viper.GetUint64("default-fabric-id")
	if fabricID == 0 {
		fabricID = 1
	}

	nodes, err := listNodesForCompletion(fabricID)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	lower := strings.ToLower(alias)
	for _, n := range nodes {
		nodeLower := strings.ToLower(n.Name)
		nodeKebab := strings.ReplaceAll(nodeLower, " ", "-")
		if nodeLower == lower || nodeKebab == lower {
			t := &Target{NodeID: n.ID}
			// Use the first non-root endpoint as default if the device has one.
			if defaultEp, ok := inferDefaultEndpoint(n); ok {
				t.Endpoint = defaultEp
				t.EndpointSet = true
			}
			return t, nil
		}
	}

	return nil, fmt.Errorf("no device named %q found (use 'matter fabric ls' to see devices)", alias)
}

// inferDefaultEndpoint picks the first non-root endpoint (ID > 0) from a
// node's endpoint list. For most Matter devices this is endpoint 1, which
// hosts the primary application cluster. Returns false if no suitable
// endpoint is found.
func inferDefaultEndpoint(node *store.Node) (uint16, bool) {
	for _, ep := range node.Endpoints {
		if ep.ID > 0 {
			return ep.ID, true
		}
	}
	return 0, false
}

// IsTargetArg reports whether the given argument looks like a @target token.
func IsTargetArg(arg string) bool {
	return strings.HasPrefix(arg, "@") && len(arg) > 1
}

// ExtractTargetFromArgs scans the argument list for a @target token, parses
// it, removes it from the args, and returns the modified args along with the
// parsed target (which may be nil if no @target was found).
//
// If a @target token is found but cannot be parsed, it is left in the args
// so that cobra produces a normal "unknown command" error rather than
// silently swallowing a typo.
func ExtractTargetFromArgs(args []string) ([]string, *Target) {
	for i, arg := range args {
		if !IsTargetArg(arg) {
			continue
		}
		// Don't extract @target if it appears after a "--" separator.
		if isPastDoubleDash(args, i) {
			continue
		}

		t, err := ParseTarget(arg)
		if err != nil {
			// Could not parse — leave it for cobra to report as an error.
			return args, nil
		}

		// Remove the @target from args.
		remaining := make([]string, 0, len(args)-1)
		remaining = append(remaining, args[:i]...)
		remaining = append(remaining, args[i+1:]...)
		return remaining, t
	}
	return args, nil
}

// isPastDoubleDash reports whether args[idx] appears after a "--" argument,
// which conventionally marks the end of flags.
func isPastDoubleDash(args []string, idx int) bool {
	for i := 0; i < idx; i++ {
		if args[i] == "--" {
			return true
		}
	}
	return false
}

// resolveTarget applies the target resolution chain in priority order and
// stores the result in the package-level resolvedTarget variable:
//
//  1. @target extracted from os.Args (stored in extractedTarget)
//  2. MATTER_TARGET environment variable
//  3. Sticky default from config (default-node / default-endpoint)
//
// This is called from PersistentPreRunE on the root command so that all
// subcommands benefit from target resolution without any changes.
func resolveTarget() {
	// Reset from any previous invocation (relevant in tests / REPL mode).
	resolvedTarget = nil

	// Priority 1: @target extracted from os.Args before cobra parsed.
	if extractedTarget != nil {
		resolvedTarget = extractedTarget
		return
	}

	// Priority 2: MATTER_TARGET environment variable.
	if envTarget := os.Getenv("MATTER_TARGET"); envTarget != "" {
		t, err := ParseTarget(envTarget)
		if err == nil {
			resolvedTarget = t
			return
		}
		// Invalid env var — ignore silently, fall through to config.
	}

	// Priority 3: sticky defaults from config file.
	if defaultNode := viper.GetUint64("default-node"); defaultNode != 0 {
		t := &Target{NodeID: defaultNode}
		if defaultEp := viper.GetUint64("default-endpoint"); defaultEp != 0 {
			t.Endpoint = uint16(defaultEp)
			t.EndpointSet = true
			// The user explicitly chose this endpoint via `matter use @node/N`,
			// so treat it as explicit for completion-filtering purposes.
			t.ExplicitEndpoint = true
		}
		resolvedTarget = t
	}
}

// requireTarget returns the resolved device target (node ID and endpoint).
// If no target has been resolved, it returns a helpful error listing all the
// ways to specify a target.
//
// Commands that need a device target should call this at the top of their
// RunE function:
//
//	nodeID, endpoint, err := requireTarget(cmd)
//	if err != nil {
//	    return err
//	}
//
// The cmd parameter is accepted for call-site compatibility but is not used;
// target resolution always reads from the resolvedTarget package variable set
// during PersistentPreRunE.
func requireTarget(_ *cobra.Command) (uint64, uint16, error) {
	if resolvedTarget == nil || resolvedTarget.NodeID == 0 {
		return 0, 0, errors.New(noTargetError)
	}
	return resolvedTarget.NodeID, resolvedTarget.Endpoint, nil
}

// targetHint returns a short string showing the resolved target for use in
// log messages and stepper output, e.g. "@1/1" or "@kitchen/2".
func targetHint(nodeID uint64, endpoint uint16) string {
	return fmt.Sprintf("@%d/%d", nodeID, endpoint)
}
