// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionNames strips the "\tdescription" suffix cobra uses so tests can
// compare bare candidate names.
func completionNames(candidates []string) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		name, _, _ := strings.Cut(c, "\t")
		out = append(out, name)
	}
	return out
}

// shorthandSubcommand finds the read/write/subscribe subcommand of a shorthand
// cluster command, which is where attribute completion is registered.
func shorthandSubcommand(t *testing.T, clusterName, verb string) *cobra.Command {
	t.Helper()
	for _, sh := range shorthandCmds {
		if sh.Name() != clusterName {
			continue
		}
		for _, sub := range sh.Commands() {
			if sub.Name() == verb {
				return sub
			}
		}
	}
	t.Fatalf("shorthand command %s %s not found", clusterName, verb)
	return nil
}

// restoreHiddenState snapshots the Hidden flag of every root and shorthand
// command so a test that calls filterShorthandCommands cannot leak hidden
// commands into sibling tests.
func restoreHiddenState(t *testing.T) {
	t.Helper()
	type hiddenState struct {
		cmd    *cobra.Command
		hidden bool
	}
	var snapshot []hiddenState
	for _, cmd := range rootCmd.Commands() {
		snapshot = append(snapshot, hiddenState{cmd, cmd.Hidden})
	}
	for _, cmd := range shorthandCmds {
		snapshot = append(snapshot, hiddenState{cmd, cmd.Hidden})
	}
	t.Cleanup(func() {
		for _, s := range snapshot {
			s.cmd.Hidden = s.hidden
		}
	})
}

// setAttributeCache installs a completion cache for the duration of a test.
func setAttributeCache(t *testing.T, cache map[uint32]map[uint32]bool) {
	t.Helper()
	prev := targetEndpointAttributeIDs
	targetEndpointAttributeIDs = cache
	t.Cleanup(func() { targetEndpointAttributeIDs = prev })
}

// TestCompletionAttributeFilter covers the cold-cache/populated-cache contract:
// a cluster with no cached list yields nil (meaning "do not filter"), so a user
// who has never run `cluster discover` keeps today's full-spec completions.
func TestCompletionAttributeFilter(t *testing.T) {
	t.Run("cold cache returns nil", func(t *testing.T) {
		setAttributeCache(t, nil)
		assert.Nil(t, completionAttributeFilter(0x0006))
	})

	t.Run("cluster missing from a populated cache returns nil", func(t *testing.T) {
		setAttributeCache(t, map[uint32]map[uint32]bool{0x0008: {0x0000: true}})
		assert.Nil(t, completionAttributeFilter(0x0006),
			"a cluster whose list was never read must fall back to the spec list")
	})

	t.Run("populated cluster returns its set", func(t *testing.T) {
		setAttributeCache(t, map[uint32]map[uint32]bool{0x0006: {0x0000: true, 0xFFFD: true}})
		got := completionAttributeFilter(0x0006)
		require.NotNil(t, got)
		assert.Equal(t, map[uint32]bool{0x0000: true, 0xFFFD: true}, got)
	})

	t.Run("empty cached list is authoritative", func(t *testing.T) {
		setAttributeCache(t, map[uint32]map[uint32]bool{0x0006: {}})
		got := completionAttributeFilter(0x0006)
		require.NotNil(t, got, "an empty-but-present list must filter, not fall back")
		assert.Empty(t, got)
	})
}

// TestFilterShorthandCommandsResetsAttributeCache pins the per-invocation reset:
// the filter is package state shared across a REPL session, so a target that
// carries no attribute scope must clear whatever the previous target left.
func TestFilterShorthandCommandsResetsAttributeCache(t *testing.T) {
	restoreHiddenState(t)
	setAttributeCache(t, map[uint32]map[uint32]bool{0x0006: {0x0000: true}})

	filterShorthandCommands(nil)
	assert.Nil(t, targetEndpointAttributeIDs,
		"a nil target must clear stale per-invocation state")

	targetEndpointAttributeIDs = map[uint32]map[uint32]bool{0x0006: {0x0000: true}}
	filterShorthandCommands(&Target{NodeID: 1})
	assert.Nil(t, targetEndpointAttributeIDs,
		"a node-only target has no endpoint, so no attribute scope applies")
}

// TestShorthandAttributeCompletionRespectsCache exercises the shorthand
// `matter @N/E OnOff read <TAB>` / `write <TAB>` / `subscribe <TAB>` paths.
func TestShorthandAttributeCompletionRespectsCache(t *testing.T) {
	onOff, ok := clusters.Global.ClusterByName("OnOff")
	require.True(t, ok)
	onTime, ok := clusters.Global.AttributeByName(onOff.ID, "OnTime")
	require.True(t, ok)
	require.True(t, onTime.Writable, "OnTime is the writable attribute this test relies on")

	for _, verb := range []string{"read", "subscribe"} {
		t.Run(verb+" cold cache offers the full spec list", func(t *testing.T) {
			setAttributeCache(t, nil)
			sub := shorthandSubcommand(t, onOff.Name, verb)
			got, _ := sub.ValidArgsFunction(sub, nil, "")
			assert.Greater(t, len(got), 1)
			assert.Contains(t, completionNames(got), "OnOff")
			assert.Contains(t, completionNames(got), "OnTime")
		})

		t.Run(verb+" populated cache offers only advertised attributes", func(t *testing.T) {
			setAttributeCache(t, map[uint32]map[uint32]bool{onOff.ID: {0x0000: true}})
			sub := shorthandSubcommand(t, onOff.Name, verb)
			got, _ := sub.ValidArgsFunction(sub, nil, "")
			assert.Equal(t, []string{"OnOff"}, completionNames(got))
		})

		t.Run(verb+" empty cached list offers nothing", func(t *testing.T) {
			setAttributeCache(t, map[uint32]map[uint32]bool{onOff.ID: {}})
			sub := shorthandSubcommand(t, onOff.Name, verb)
			got, _ := sub.ValidArgsFunction(sub, nil, "")
			assert.Empty(t, got)
		})
	}

	t.Run("write cold cache offers writable spec attributes", func(t *testing.T) {
		setAttributeCache(t, nil)
		sub := shorthandSubcommand(t, onOff.Name, "write")
		got, _ := sub.ValidArgsFunction(sub, nil, "")
		names := completionNames(got)
		assert.Contains(t, names, "OnTime")
		assert.NotContains(t, names, "OnOff", "OnOff is read-only")
	})

	t.Run("write intersects writability with the cache", func(t *testing.T) {
		// The device advertises OnOff (read-only) and OnTime (writable).
		setAttributeCache(t, map[uint32]map[uint32]bool{onOff.ID: {0x0000: true, onTime.ID: true}})
		sub := shorthandSubcommand(t, onOff.Name, "write")
		got, _ := sub.ValidArgsFunction(sub, nil, "")
		assert.Equal(t, []string{"OnTime"}, completionNames(got))
	})

	t.Run("write drops writable attributes the device does not advertise", func(t *testing.T) {
		setAttributeCache(t, map[uint32]map[uint32]bool{onOff.ID: {0x0000: true}})
		sub := shorthandSubcommand(t, onOff.Name, "write")
		got, _ := sub.ValidArgsFunction(sub, nil, "")
		assert.Empty(t, got)
	})
}

// TestShorthandAttributeCompletionStopsAfterFirstArg keeps the pre-existing
// behaviour that only the attribute position completes.
func TestShorthandAttributeCompletionStopsAfterFirstArg(t *testing.T) {
	setAttributeCache(t, nil)
	for _, verb := range []string{"read", "subscribe", "write"} {
		sub := shorthandSubcommand(t, "OnOff", verb)
		got, _ := sub.ValidArgsFunction(sub, []string{"OnOff"}, "")
		assert.Empty(t, got, "%s should not complete past the attribute argument", verb)
	}
}

// TestGenericAttributeFlagCompletionRespectsCache exercises the
// `--attribute <TAB>` form of read, write, and subscribe.
func TestGenericAttributeFlagCompletionRespectsCache(t *testing.T) {
	onOff, ok := clusters.Global.ClusterByName("OnOff")
	require.True(t, ok)

	cases := []struct {
		verb string
		cmd  func() *cobra.Command
	}{
		{"read", newClusterReadCmd},
		{"write", newClusterWriteCmd},
		{"subscribe", newClusterSubscribeCmd},
	}

	for _, c := range cases {
		t.Run(c.verb+" cold cache falls back to the spec list", func(t *testing.T) {
			setAttributeCache(t, nil)
			cmd := c.cmd()
			require.NoError(t, cmd.Flags().Set("cluster", "OnOff"))
			fn := attributeFlagCompletion(t, cmd)
			got, _ := fn(cmd, nil, "")
			assert.Contains(t, completionNames(got), "OnTime")
		})

		t.Run(c.verb+" populated cache filters to advertised attributes", func(t *testing.T) {
			setAttributeCache(t, map[uint32]map[uint32]bool{onOff.ID: {0x0000: true}})
			cmd := c.cmd()
			require.NoError(t, cmd.Flags().Set("cluster", "OnOff"))
			fn := attributeFlagCompletion(t, cmd)
			got, _ := fn(cmd, nil, "")
			assert.NotContains(t, completionNames(got), "OnTime",
				"%s should not offer an attribute the device does not advertise", c.verb)
		})
	}
}

// attributeFlagCompletion returns the completion function the command actually
// registered for its --attribute flag, so these tests exercise the real wiring
// rather than a re-implementation of it.
func attributeFlagCompletion(t *testing.T, cmd *cobra.Command) cobra.CompletionFunc {
	t.Helper()
	fn, ok := cmd.GetFlagCompletionFunc("attribute")
	require.True(t, ok, "command %q must register a completion func for --attribute", cmd.Name())
	return fn
}
