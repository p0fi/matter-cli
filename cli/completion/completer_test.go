// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// setupTestStore creates a temporary BoltDB store with the given nodes and
// configures the environment so that listNodes() will find it. Returns a
// cleanup function that must be called when done.
func setupTestStore(t *testing.T, fabricID uint64, nodes []*store.Node) func() {
	t.Helper()

	dir := t.TempDir()
	dbDir := filepath.Join(dir, "matter-cli")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("creating test db dir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "matter.db")

	s, err := store.NewBoltStore(dbPath)
	if err != nil {
		t.Fatalf("creating test BoltStore: %v", err)
	}

	// SaveNode requires the fabric to exist first.
	if err := s.SaveFabric(&store.Fabric{ID: fabricID, Label: "test"}); err != nil {
		s.Close()
		t.Fatalf("saving test fabric: %v", err)
	}

	for _, n := range nodes {
		n.FabricID = fabricID
		if err := s.SaveNode(fabricID, n); err != nil {
			s.Close()
			t.Fatalf("saving test node %d: %v", n.ID, err)
		}
	}
	s.Close()

	// Point the store at our temp dir and configure viper with the fabric ID.
	t.Setenv("XDG_CONFIG_HOME", dir)
	viper.Set("default-fabric-id", fabricID)

	return func() {
		viper.Set("default-fabric-id", nil)
	}
}

// testNodes returns a small fleet of nodes useful for completion tests.
func testNodes() []*store.Node {
	return []*store.Node{
		{
			ID:   1,
			Name: "Kitchen Light",
			Endpoints: []store.Endpoint{
				{ID: 0, DeviceTypes: []store.DeviceType{{ID: 0x0016}}}, // root
				{
					ID:          1,
					DeviceTypes: []store.DeviceType{{ID: 0x0100}}, // On/Off Light
					Clusters: []store.ClusterRef{
						{ID: 0x0006, Name: "on-off", Side: "server"},
					},
				},
			},
		},
		{
			ID:   2,
			Name: "Front Door Lock",
			Endpoints: []store.Endpoint{
				{ID: 0, DeviceTypes: []store.DeviceType{{ID: 0x0016}}},
				{
					ID:          1,
					DeviceTypes: []store.DeviceType{{ID: 0x000A}}, // Door Lock
					Clusters: []store.ClusterRef{
						{ID: 0x0101, Name: "door-lock", Side: "server"},
					},
				},
				{
					ID:          2,
					DeviceTypes: []store.DeviceType{{ID: 0x000A}},
					Clusters:    []store.ClusterRef{{ID: 0x0006, Name: "on-off", Side: "server"}},
				},
			},
		},
	}
}

func runTargetCompletion(t *testing.T, toComplete string) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	fn := TargetCompletionFunc()
	cmd := &cobra.Command{Use: "test"}
	completions, directive := fn(cmd, nil, toComplete)
	return completions, directive
}

// TestTargetCompletionFunc_Stage1_NodeOnly verifies that completions for "@"
// (or a partial node prefix) return node-level tokens without endpoint suffixes,
// include the ShellCompDirectiveNoSpace flag, and contain no bare-numeric @N
// forms for nodes that have a name.
func TestTargetCompletionFunc_Stage1_NodeOnly(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	cases := []struct {
		toComplete  string
		wantNoSpace bool
	}{
		{"@", true},
		{"@ki", true},
		{"@kitchen", true},
		{"@f", true},
	}

	for _, tc := range cases {
		t.Run(tc.toComplete, func(t *testing.T) {
			completions, directive := runTargetCompletion(t, tc.toComplete)

			if len(completions) == 0 {
				t.Fatalf("expected completions for %q, got none", tc.toComplete)
			}

			for _, c := range completions {
				token := strings.SplitN(c, "\t", 2)[0]
				// All entries must be node-level (no "/" in the completion token).
				if strings.Contains(token, "/") {
					t.Errorf("stage-1 completion %q contains '/', want node-only", token)
				}
				if !strings.HasPrefix(token, "@") {
					t.Errorf("completion token %q does not start with '@'", token)
				}
			}

			hasNoSpace := directive&cobra.ShellCompDirectiveNoSpace != 0
			if hasNoSpace != tc.wantNoSpace {
				t.Errorf("ShellCompDirectiveNoSpace = %v, want %v (directive=%d)", hasNoSpace, tc.wantNoSpace, directive)
			}
		})
	}
}

// TestTargetCompletionFunc_Stage1_NoNumericDuplicates verifies that stage-1
// does not emit a bare @N numeric token alongside a named alias for nodes
// whose alias is unique in the fabric.
func TestTargetCompletionFunc_Stage1_NoNumericDuplicates(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	completions, _ := runTargetCompletion(t, "@")

	tokens := make(map[string]int) // token → count
	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		tokens[token]++
	}

	// @1 and @2 are the numeric forms of uniquely-named nodes; they must not
	// appear because @kitchen-light and @front-door-lock are unambiguous.
	for _, numeric := range []string{"@1", "@2"} {
		if tokens[numeric] > 0 {
			t.Errorf("stage-1 emitted numeric token %q for a uniquely-named node; want named alias only", numeric)
		}
	}

	// Named aliases must appear exactly once each.
	for _, alias := range []string{"@kitchen-light", "@front-door-lock"} {
		if tokens[alias] != 1 {
			t.Errorf("expected exactly 1 completion for %q, got %d", alias, tokens[alias])
		}
	}
}

// TestTargetCompletionFunc_Stage1_DuplicateAlias verifies that when multiple
// nodes share the same name, stage-1 emits unique @N numeric tokens (not
// @alias which would be deduplicated by zsh).
func TestTargetCompletionFunc_Stage1_DuplicateAlias(t *testing.T) {
	// Three nodes sharing the same name.
	nodes := []*store.Node{
		{
			ID:   10,
			Name: "Smart Plug",
			Endpoints: []store.Endpoint{
				{ID: 0},
				{ID: 1, DeviceTypes: []store.DeviceType{{ID: 0x010A}}},
			},
		},
		{
			ID:   11,
			Name: "Smart Plug",
			Endpoints: []store.Endpoint{
				{ID: 0},
				{ID: 1, DeviceTypes: []store.DeviceType{{ID: 0x010A}}},
			},
		},
		{
			ID:   12,
			Name: "Smart Plug",
			Endpoints: []store.Endpoint{
				{ID: 0},
				{ID: 1, DeviceTypes: []store.DeviceType{{ID: 0x010A}}},
			},
		},
	}
	cleanup := setupTestStore(t, 1, nodes)
	defer cleanup()

	completions, _ := runTargetCompletion(t, "@")

	if len(completions) != 3 {
		t.Fatalf("expected 3 completions for 3 duplicate-named nodes, got %d: %v", len(completions), completions)
	}

	tokens := make(map[string]bool)
	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		if tokens[token] {
			t.Errorf("duplicate completion token %q emitted twice", token)
		}
		tokens[token] = true
		// Each token must be the numeric @N form, not the shared alias.
		if token == "@smart-plug" {
			t.Errorf("got shared alias token %q; want unique @N numeric tokens", token)
		}
	}

	// All three numeric tokens must be present.
	for _, want := range []string{"@10", "@11", "@12"} {
		if !tokens[want] {
			t.Errorf("expected numeric token %q in completions, got %v", want, completions)
		}
	}

	// Each description must be unique (so zsh shows a vertical list, not a
	// horizontal grouped row).
	descs := make(map[string]bool)
	for _, c := range completions {
		parts := strings.SplitN(c, "\t", 2)
		if len(parts) == 2 {
			if descs[parts[1]] {
				t.Errorf("duplicate description %q; want all descriptions unique for vertical display", parts[1])
			}
			descs[parts[1]] = true
		}
	}
}

// TestTargetCompletionFunc_Stage1_DescriptionFormat verifies that named node
// completions lead their description with the node ID for quick reference.
func TestTargetCompletionFunc_Stage1_DescriptionFormat(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	completions, _ := runTargetCompletion(t, "@kitchen")

	if len(completions) == 0 {
		t.Fatal("expected completions for @kitchen, got none")
	}

	for _, c := range completions {
		parts := strings.SplitN(c, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("completion entry missing tab separator: %q", c)
		}
		desc := parts[1]
		// Description must start with the numeric node ID, not a bracket.
		if strings.HasPrefix(desc, "[") {
			t.Errorf("description %q still uses bracket format; want plain node-ID prefix", desc)
		}
		// Description must start with a digit (the node ID).
		if len(desc) == 0 || desc[0] < '0' || desc[0] > '9' {
			t.Errorf("description %q does not start with node ID digit", desc)
		}
	}
}

// TestTargetCompletionFunc_Stage2_Endpoints verifies that once a "/" is typed,
// completions switch to endpoint-level tokens (@node/N) without NoSpace.
func TestTargetCompletionFunc_Stage2_Endpoints(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	cases := []struct {
		toComplete    string
		wantTokens    []string // substrings that must appear in the tokens
		wantNoEndpointTokens bool // true means no tokens without "/" are expected
	}{
		{
			toComplete: "@kitchen-light/",
			wantTokens: []string{"@kitchen-light/1"},
		},
		{
			// Numeric prefix → completions must use @N/ep, not @alias/ep.
			toComplete: "@1/",
			wantTokens: []string{"@1/1"},
		},
		{
			toComplete: "@front-door-lock/",
			wantTokens: []string{"/1", "/2"},
		},
		{
			toComplete: "@2/1",
			wantTokens: []string{"@2/1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.toComplete, func(t *testing.T) {
			completions, directive := runTargetCompletion(t, tc.toComplete)

			// All tokens must contain "/" — stage 2.
			for _, c := range completions {
				token := strings.SplitN(c, "\t", 2)[0]
				if !strings.Contains(token, "/") {
					t.Errorf("stage-2 completion %q is missing '/', want @node/endpoint", token)
				}
			}

			// NoSpace must NOT be set for stage-2 (endpoint selected → add space).
			hasNoSpace := directive&cobra.ShellCompDirectiveNoSpace != 0
			if hasNoSpace {
				t.Errorf("stage-2 should not include ShellCompDirectiveNoSpace")
			}

			// Check that expected token substrings appear.
			allTokens := make([]string, 0, len(completions))
			for _, c := range completions {
				allTokens = append(allTokens, strings.SplitN(c, "\t", 2)[0])
			}
			for _, want := range tc.wantTokens {
				found := false
				for _, tok := range allTokens {
					if strings.Contains(tok, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected a token containing %q, got %v", want, allTokens)
				}
			}
		})
	}
}

// TestTargetCompletionFunc_NoAt verifies that non-@ input returns nil.
func TestTargetCompletionFunc_NoAt(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	completions, _ := runTargetCompletion(t, "OnOff")
	if completions != nil {
		t.Errorf("expected nil completions for non-@ input, got %v", completions)
	}

	completions, _ = runTargetCompletion(t, "")
	if completions != nil {
		t.Errorf("expected nil completions for empty input, got %v", completions)
	}
}

// TestTargetCompletionFunc_NoMatchingNodes verifies that a prefix that doesn't
// match any node returns an empty list (not an error).
func TestTargetCompletionFunc_NoMatchingNodes(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	completions, directive := runTargetCompletion(t, "@zzznomatch")
	if len(completions) != 0 {
		t.Errorf("expected no completions for unmatched prefix, got %v", completions)
	}
	// Should still return a valid (non-error) directive.
	if directive&cobra.ShellCompDirectiveError != 0 {
		t.Errorf("unexpected ShellCompDirectiveError for unmatched prefix")
	}
}

// testRegistry returns a cluster registry populated with a small set of test
// clusters for use in completion tests.
func testRegistry() *clusters.Registry {
	r := clusters.NewRegistry()
	r.Register(clusters.ClusterInfo{ID: 0x0006, Name: "OnOff", DisplayName: "On/Off"})
	r.Register(clusters.ClusterInfo{ID: 0x0008, Name: "LevelControl", DisplayName: "Level Control"})
	r.Register(clusters.ClusterInfo{ID: 0x0300, Name: "ColorControl", DisplayName: "Color Control"})
	return r
}

// TestRootCompletionFunc_ClusterShorthand verifies that non-@ input is matched
// against the cluster registry in a case-insensitive way.
func TestRootCompletionFunc_ClusterShorthand(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	reg := testRegistry()
	fn := RootCompletionFunc(reg)
	cmd := &cobra.Command{Use: "test"}

	cases := []struct {
		toComplete string
		wantNames  []string // PascalCase names that must appear in results
	}{
		{"on", []string{"OnOff"}},
		{"On", []string{"OnOff"}},
		{"ON", []string{"OnOff"}},
		{"level", []string{"LevelControl"}},
		{"LEVEL", []string{"LevelControl"}},
		{"control", []string{"LevelControl", "ColorControl"}},
		{"color", []string{"ColorControl"}},
		{"", []string{"OnOff", "LevelControl", "ColorControl"}}, // empty returns all
	}

	for _, tc := range cases {
		t.Run(tc.toComplete, func(t *testing.T) {
			completions, directive := fn(cmd, nil, tc.toComplete)
			if directive&cobra.ShellCompDirectiveError != 0 {
				t.Fatalf("unexpected ShellCompDirectiveError for %q", tc.toComplete)
			}
			// Build a set of returned completion tokens (strip tab-separated description).
			got := make(map[string]bool, len(completions))
			for _, c := range completions {
				token := strings.SplitN(c, "\t", 2)[0]
				got[token] = true
			}
			for _, want := range tc.wantNames {
				if !got[want] {
					t.Errorf("toComplete=%q: want %q in completions, got %v", tc.toComplete, want, completions)
				}
			}
		})
	}
}

// TestRootCompletionFunc_ClusterShorthand_NoMatch verifies that an unmatched
// query returns nil completions (not an error).
func TestRootCompletionFunc_ClusterShorthand_NoMatch(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	reg := testRegistry()
	fn := RootCompletionFunc(reg)
	cmd := &cobra.Command{Use: "test"}

	completions, directive := fn(cmd, nil, "zzznomatch")
	if completions != nil {
		t.Errorf("expected nil completions for unmatched query, got %v", completions)
	}
	if directive&cobra.ShellCompDirectiveError != 0 {
		t.Errorf("unexpected ShellCompDirectiveError")
	}
}

// TestRootCompletionFunc_AtTarget verifies that @-prefixed input is still
// handled as target completion (not cluster completion).
func TestRootCompletionFunc_AtTarget(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	reg := testRegistry()
	fn := RootCompletionFunc(reg)
	cmd := &cobra.Command{Use: "test"}

	completions, _ := fn(cmd, nil, "@")
	// Should return target completions, not cluster completions.
	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		if !strings.HasPrefix(token, "@") {
			t.Errorf("expected @-prefixed token, got %q", token)
		}
	}
}

// TestNodeSummary ensures the helper returns a non-empty string for a node
// that has a recognisable device type on its first non-root endpoint.
func TestNodeSummary(t *testing.T) {
	n := &store.Node{
		ID:   1,
		Name: "Kitchen Light",
		Endpoints: []store.Endpoint{
			{ID: 0},
			{ID: 1, DeviceTypes: []store.DeviceType{{ID: 0x0100}}},
		},
	}
	s := nodeSummary(n)
	if s == "" {
		t.Error("nodeSummary returned empty string")
	}
}
