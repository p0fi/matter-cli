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
// configures the environment so that loadNodes() will find it. Returns a
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
	fn := TargetCompletionFunc(nil)
	cmd := &cobra.Command{Use: "test"}
	completions, directive := fn(cmd, nil, toComplete)
	return completions, directive
}

func runTargetCompletionWithCommands(
	t *testing.T, toComplete string, cmds []TopLevelCommand,
) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	// Expansion tokens are only emitted when the caller opts in via
	// ExpandEnvVar (set by the zsh completion script). t.Setenv handles
	// cleanup automatically.
	t.Setenv(ExpandEnvVar, "zsh")
	fn := TargetCompletionFunc(func() []TopLevelCommand { return cmds })
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

// TestTargetCompletionFunc_Stage1_AlwaysNumericTokens verifies that stage-1
// always emits numeric @N tokens (never named aliases), and that named nodes
// have their alias in the description.
func TestTargetCompletionFunc_Stage1_AlwaysNumericTokens(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	completions, _ := runTargetCompletion(t, "@")

	tokens := make(map[string]int) // token → count
	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		tokens[token]++
	}

	// Named aliases must NOT appear as tokens — always numeric.
	for _, alias := range []string{"@kitchen-light", "@front-door-lock"} {
		if tokens[alias] > 0 {
			t.Errorf("stage-1 emitted named alias token %q; want numeric @N tokens only", alias)
		}
	}

	// Numeric tokens must appear exactly once each.
	for _, numeric := range []string{"@1", "@2"} {
		if tokens[numeric] != 1 {
			t.Errorf("expected exactly 1 completion for %q, got %d", numeric, tokens[numeric])
		}
	}

	// Named nodes must have their alias visible in the description.
	for _, c := range completions {
		parts := strings.SplitN(c, "\t", 2)
		token := parts[0]
		if len(parts) < 2 {
			continue
		}
		desc := parts[1]
		switch token {
		case "@1":
			if !strings.Contains(desc, "kitchen-light") {
				t.Errorf("description for @1 %q does not contain alias 'kitchen-light'", desc)
			}
		case "@2":
			if !strings.Contains(desc, "front-door-lock") {
				t.Errorf("description for @2 %q does not contain alias 'front-door-lock'", desc)
			}
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
// completions include both the alias and the node ID (in brackets) in the
// description, and that the token itself is numeric.
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
		token, desc := parts[0], parts[1]
		// Token must be numeric @N, not a named alias.
		if token != "@1" {
			t.Errorf("expected numeric token @1, got %q", token)
		}
		// Description must contain the alias.
		if !strings.Contains(desc, "kitchen-light") {
			t.Errorf("description %q does not contain alias 'kitchen-light'", desc)
		}
		// Description must contain the node ID in brackets.
		if !strings.Contains(desc, "[1]") {
			t.Errorf("description %q does not contain '[1]' for node ID", desc)
		}
	}
}

// TestTargetCompletionFunc_Stage2_Endpoints verifies that once a "/" is typed,
// completions switch to endpoint-level tokens (@node/N) without NoSpace.
func TestTargetCompletionFunc_Stage2_Endpoints(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	cases := []struct {
		toComplete string
		wantTokens []string // substrings that must appear in the tokens
		wantEmpty  bool     // true means no completions expected
	}{
		{
			// Alias-based prefix → no completions (only numeric @N/ep supported).
			toComplete: "@kitchen-light/",
			wantEmpty:  true,
		},
		{
			// Numeric prefix → completions use @N/ep only.
			toComplete: "@1/",
			wantTokens: []string{"@1/0", "@1/1"},
		},
		{
			// Alias-based prefix → no completions.
			toComplete: "@front-door-lock/",
			wantEmpty:  true,
		},
		{
			toComplete: "@2/1",
			wantTokens: []string{"@2/1"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.toComplete, func(t *testing.T) {
			completions, directive := runTargetCompletion(t, tc.toComplete)

			if tc.wantEmpty {
				if len(completions) != 0 {
					t.Errorf("expected no completions for alias-based prefix %q, got %v", tc.toComplete, completions)
				}
				return
			}

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
	fn := RootCompletionFunc(reg, nil, nil)
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
	fn := RootCompletionFunc(reg, nil, nil)
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
	fn := RootCompletionFunc(reg, nil, nil)
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

// TestRootCompletionFunc_ClusterFilter verifies that an allowedClusters filter
// restricts cluster completions to the returned set of IDs. This is the
// mechanism used to scope completions for `matter @N/EP <TAB>` to clusters
// actually present on the target endpoint.
func TestRootCompletionFunc_ClusterFilter(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	reg := testRegistry()
	cmd := &cobra.Command{Use: "test"}

	t.Run("nil map → all clusters", func(t *testing.T) {
		fn := RootCompletionFunc(reg, func() map[uint32]bool { return nil }, nil)
		completions, _ := fn(cmd, nil, "")
		if len(completions) != 3 {
			t.Errorf("expected 3 completions (all registry clusters), got %d: %v", len(completions), completions)
		}
	})

	t.Run("empty map → no clusters", func(t *testing.T) {
		fn := RootCompletionFunc(reg, func() map[uint32]bool { return map[uint32]bool{} }, nil)
		completions, _ := fn(cmd, nil, "")
		if len(completions) != 0 {
			t.Errorf("expected no completions for empty allow set, got %v", completions)
		}
	})

	t.Run("restricted set → only allowed clusters", func(t *testing.T) {
		allowed := map[uint32]bool{0x0006: true} // OnOff only
		fn := RootCompletionFunc(reg, func() map[uint32]bool { return allowed }, nil)
		completions, _ := fn(cmd, nil, "")
		if len(completions) != 1 {
			t.Fatalf("expected exactly 1 completion, got %v", completions)
		}
		token := strings.SplitN(completions[0], "\t", 2)[0]
		if token != "OnOff" {
			t.Errorf("expected OnOff completion, got %q", token)
		}
	})

	t.Run("filter does not affect @target completion", func(t *testing.T) {
		fn := RootCompletionFunc(reg, func() map[uint32]bool { return map[uint32]bool{} }, nil)
		completions, _ := fn(cmd, nil, "@")
		if len(completions) == 0 {
			t.Error("empty allow set should not suppress @target completions")
		}
	})
}

// TestTargetCompletionFunc_Stage1b_EndpointsInExactMatch verifies that when the
// user types an exact numeric node ID (e.g. "@1"), the completion set includes
// both the bare @N token and endpoint tokens (@N/0, @N/1) without requiring the
// user to type "/".
func TestTargetCompletionFunc_Stage1b_EndpointsInExactMatch(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	completions, directive := runTargetCompletion(t, "@1")

	if len(completions) == 0 {
		t.Fatal("expected completions for @1, got none")
	}

	// Must include NoSpace so the user can type "/" for endpoints without a
	// trailing space being inserted.
	if directive&cobra.ShellCompDirectiveNoSpace == 0 {
		t.Errorf("expected ShellCompDirectiveNoSpace for exact @N match")
	}

	tokens := make(map[string]bool)
	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		tokens[token] = true
	}

	// Node itself must be present.
	if !tokens["@1"] {
		t.Errorf("expected @1 in completions, got %v", completions)
	}

	// Endpoint tokens for node 1 (endpoints 0 and 1).
	for _, want := range []string{"@1/0", "@1/1"} {
		if !tokens[want] {
			t.Errorf("expected endpoint token %q in stage-1b completions, got %v", want, completions)
		}
	}

	// No expansion tokens expected (no topLevelCommands supplied).
	for tok := range tokens {
		if strings.Contains(tok, ExpandSeparator) {
			t.Errorf("unexpected expansion token %q without topLevelCommands", tok)
		}
	}
}

// TestTargetCompletionFunc_Stage1b_CommandsInExactMatch verifies that when a
// topLevelCommands func is provided and the user types an exact @N, the result
// set includes "@N+<cmd>" expansion tokens for each target-aware command.
func TestTargetCompletionFunc_Stage1b_CommandsInExactMatch(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	cmds := []TopLevelCommand{
		{Name: "tree", Short: "Show device tree", Group: "device", TargetAware: true},
		{Name: "OnOff", Short: "On/Off cluster", Group: "cluster", TargetAware: true},
		{Name: "code", Short: "Parse or generate pairing codes", Group: "tool", TargetAware: false},
		{Name: "commission", Short: "Commission a device", Group: "device", TargetAware: false},
	}

	completions, _ := runTargetCompletionWithCommands(t, "@1", cmds)

	tokens := make(map[string]bool)
	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		tokens[token] = true
	}

	// TargetAware commands get expansion tokens.
	if !tokens["@1"+ExpandSeparator+"tree"] {
		t.Errorf("expected @1+tree expansion token, got %v", completions)
	}
	if !tokens["@1"+ExpandSeparator+"OnOff"] {
		t.Errorf("expected @1+OnOff expansion token, got %v", completions)
	}

	// Non-TargetAware commands must NOT produce expansion tokens.
	if tokens["@1"+ExpandSeparator+"code"] {
		t.Errorf("unexpected @1+code expansion token for non-TargetAware command")
	}
	if tokens["@1"+ExpandSeparator+"commission"] {
		t.Errorf("unexpected @1+commission expansion token for non-TargetAware command")
	}
}

// TestTargetCompletionFunc_Stage1b_ExpandEnvVarGate verifies that when the
// ExpandEnvVar is not set (simulating a non-zsh shell like bash/fish), no
// "@N+<cmd>" expansion tokens are emitted even if topLevelCommands is provided.
// Those other shells do not know how to rewrite these tokens and would
// otherwise surface the literal "@N+<cmd>" text to the user.
func TestTargetCompletionFunc_Stage1b_ExpandEnvVarGate(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	// Explicitly clear the env var to simulate a non-zsh shell. t.Setenv
	// guarantees the original value is restored when the test returns.
	t.Setenv(ExpandEnvVar, "")

	cmds := []TopLevelCommand{
		{Name: "tree", Short: "Show device tree", Group: "device", TargetAware: true},
	}
	fn := TargetCompletionFunc(func() []TopLevelCommand { return cmds })
	cmd := &cobra.Command{Use: "test"}
	completions, _ := fn(cmd, nil, "@1")

	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		if strings.Contains(token, ExpandSeparator) {
			t.Errorf("unexpected expansion token %q when ExpandEnvVar is unset", token)
		}
	}
}

// TestTargetCompletionFunc_Stage1b_NamedNoExpansion verifies that alias-based
// prefixes (e.g. "@kitchen") do NOT trigger stage-1b expansion even when they
// fully match a named node — expansion only fires for numeric IDs.
func TestTargetCompletionFunc_Stage1b_NamedNoExpansion(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	cmds := []TopLevelCommand{
		{Name: "tree", Short: "Show device tree", Group: "device", TargetAware: true},
	}

	completions, _ := runTargetCompletionWithCommands(t, "@kitchen-light", cmds)

	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		if strings.Contains(token, ExpandSeparator) {
			t.Errorf("unexpected expansion token %q for alias-based prefix", token)
		}
		if strings.Contains(token, "/") {
			t.Errorf("unexpected endpoint token %q for alias-based prefix", token)
		}
	}
}

// TestTargetCompletionFunc_Stage1b_NonExistentNodeNoExpansion verifies that a
// numeric prefix that does not match any commissioned node produces no expansion
// tokens and no endpoint tokens.
func TestTargetCompletionFunc_Stage1b_NonExistentNodeNoExpansion(t *testing.T) {
	cleanup := setupTestStore(t, 1, testNodes())
	defer cleanup()

	cmds := []TopLevelCommand{
		{Name: "tree", Short: "Show device tree", Group: "device", TargetAware: true},
	}

	// Node 99 doesn't exist in the test store.
	completions, _ := runTargetCompletionWithCommands(t, "@99", cmds)

	for _, c := range completions {
		token := strings.SplitN(c, "\t", 2)[0]
		if strings.Contains(token, ExpandSeparator) {
			t.Errorf("unexpected expansion token %q for non-existent node", token)
		}
		if strings.Contains(token, "/") {
			t.Errorf("unexpected endpoint token %q for non-existent node", token)
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
