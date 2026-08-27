// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"

	"github.com/spf13/cobra"
)

func TestValidateSubscribeIntervals(t *testing.T) {
	tests := []struct {
		name    string
		min     uint16
		max     uint16
		wantErr bool
	}{
		{"zero min allowed", 0, 10, false},
		{"zero max rejected", 5, 0, true},
		{"equal bounds allowed", 10, 10, false},
		{"inverted bounds rejected", 10, 5, true},
		{"default 1..60", 1, 60, false},
		{"uint16 max bound", 0, 65535, false},
		{"min at uint16 max, max below it", 65535, 100, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubscribeIntervals(tt.min, tt.max)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSubscribeIntervals(%d, %d) error = %v, wantErr %v", tt.min, tt.max, err, tt.wantErr)
			}
		})
	}
}

func TestSubscribeBoundReached(t *testing.T) {
	tests := []struct {
		name    string
		count   uint32
		emitted uint32
		want    bool
	}{
		{"unlimited never reached", 0, 1000, false},
		{"below count", 5, 4, false},
		{"exactly at count", 5, 5, true},
		{"past count", 5, 6, true},
		{"count one, priming only", 1, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subscribeBoundReached(tt.count, tt.emitted); got != tt.want {
				t.Errorf("subscribeBoundReached(%d, %d) = %v, want %v", tt.count, tt.emitted, got, tt.want)
			}
		})
	}
}

func TestDecodeTLVNative(t *testing.T) {
	t.Run("bool", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutBool(tlv.AnonymousTag(), true); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if b, ok := v.(bool); !ok || !b {
			t.Errorf("decoded = %#v, want true", v)
		}
	})

	t.Run("unsigned int", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), 42); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if n, ok := v.(uint64); !ok || n != 42 {
			t.Errorf("decoded = %#v, want uint64(42)", v)
		}
	})

	t.Run("signed int", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutSignedInt(tlv.AnonymousTag(), -7); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if n, ok := v.(int64); !ok || n != -7 {
			t.Errorf("decoded = %#v, want int64(-7)", v)
		}
	})

	t.Run("float64", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutFloat64(tlv.AnonymousTag(), 3.5); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if f, ok := v.(float64); !ok || f != 3.5 {
			t.Errorf("decoded = %#v, want float64(3.5)", v)
		}
	})

	t.Run("string", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutUTF8String(tlv.AnonymousTag(), "hello"); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := v.(string); !ok || s != "hello" {
			t.Errorf("decoded = %#v, want %q", v, "hello")
		}
	})

	t.Run("octet string as hex", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutOctetString(tlv.AnonymousTag(), []byte{0xDE, 0xAD, 0xBE, 0xEF}); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := v.(string); !ok || s != "0xdeadbeef" {
			t.Errorf("decoded = %#v, want %q", v, "0xdeadbeef")
		}
	})

	t.Run("null", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutNull(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if v != nil {
			t.Errorf("decoded = %#v, want nil", v)
		}
	})

	t.Run("array", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartArray(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), 1); err != nil {
			t.Fatal(err)
		}
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), 2); err != nil {
			t.Fatal(err)
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		arr, ok := v.([]any)
		if !ok || len(arr) != 2 {
			t.Fatalf("decoded = %#v, want a 2-element []any", v)
		}
		if arr[0].(uint64) != 1 || arr[1].(uint64) != 2 {
			t.Errorf("decoded array = %#v, want [1 2]", arr)
		}
	})

	t.Run("struct keyed by tag number", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.StartStructure(tlv.AnonymousTag()); err != nil {
			t.Fatal(err)
		}
		if err := w.PutBool(tlv.ContextTag(0), true); err != nil {
			t.Fatal(err)
		}
		if err := w.PutUTF8String(tlv.ContextTag(1), "x"); err != nil {
			t.Fatal(err)
		}
		if err := w.EndContainer(); err != nil {
			t.Fatal(err)
		}
		v, err := decodeTLVNative(w.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("decoded = %#v, want map[string]any", v)
		}
		if m["0"] != true {
			t.Errorf("m[0] = %#v, want true", m["0"])
		}
		if m["1"] != "x" {
			t.Errorf("m[1] = %#v, want \"x\"", m["1"])
		}
	})

	t.Run("empty payload errors", func(t *testing.T) {
		if _, err := decodeTLVNative(nil); err == nil {
			t.Error("expected an error for an empty payload")
		}
	})

	t.Run("malformed payload errors", func(t *testing.T) {
		if _, err := decodeTLVNative([]byte{0x15, 0x24}); err == nil {
			t.Error("expected an error for a truncated structure")
		}
	})
}

// ---------------------------------------------------------------------------
// Command-level tests: generic `cluster subscribe`
//
// These drive the command's own RunE directly (bypassing rootCmd's
// PersistentPreRunE/Execute machinery, consistent with how other cli_test.go
// files exercise resolvedTarget-dependent logic — see rename_test.go,
// commission_ble_test.go, target_test.go) and only exercise the branches
// that return before connectToNode ever runs a real network/store
// operation: missing target, missing/unknown selectors, and interval
// validation. The "everything valid" path is intentionally not driven here,
// since it would require a real device connection.
// ---------------------------------------------------------------------------

func TestNewClusterSubscribeCmd_FlagsAndAnnotation(t *testing.T) {
	cmd := newClusterSubscribeCmd()

	wantFlags := map[string]struct{ shorthand, defValue string }{
		"cluster":   {"C", ""},
		"attribute": {"a", ""},
		"min":       {"m", "1"},
		"max":       {"M", "60"},
		"count":     {"n", "0"},
		"duration":  {"d", "0s"},
	}
	for name, want := range wantFlags {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("flag %q not registered", name)
		}
		if f.Shorthand != want.shorthand {
			t.Errorf("flag %q shorthand = %q, want %q", name, f.Shorthand, want.shorthand)
		}
		if f.DefValue != want.defValue {
			t.Errorf("flag %q default = %q, want %q", name, f.DefValue, want.defValue)
		}
	}

	if cmd.Example == "" {
		t.Error("generic subscribe command should have an Example for discoverability")
	}
	if cmd.Annotations[bypassDaemonAnnotation] != "true" {
		t.Error("generic subscribe command must carry bypassDaemonAnnotation so -K never spawns a daemon for it")
	}
	if _, ok := cmd.GetFlagCompletionFunc("cluster"); !ok {
		t.Error("--cluster should have a registered completion function")
	}
	if _, ok := cmd.GetFlagCompletionFunc("attribute"); !ok {
		t.Error("--attribute should have a registered completion function")
	}
}

func TestNewClusterSubscribeCmd_RequiresTarget(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = nil
	defer func() { resolvedTarget = prev }()

	cmd := newClusterSubscribeCmd()
	_ = cmd.Flags().Set("cluster", "OnOff")
	_ = cmd.Flags().Set("attribute", "OnOff")

	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "no target specified") {
		t.Errorf("RunE() error = %v, want the no-target error", err)
	}
}

func TestNewClusterSubscribeCmd_SelectorResolution(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 1, Endpoint: 1, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	tests := []struct {
		name        string
		cluster     string
		attribute   string
		wantErrText string
	}{
		{"missing cluster", "", "OnOff", "--cluster is required"},
		{"missing attribute", "OnOff", "", "--attribute is required"},
		{"unknown cluster", "NotACluster", "OnOff", `unknown cluster "NotACluster"`},
		{"unknown attribute", "OnOff", "NotAnAttribute", `unknown attribute "NotAnAttribute"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newClusterSubscribeCmd()
			if tt.cluster != "" {
				_ = cmd.Flags().Set("cluster", tt.cluster)
			}
			if tt.attribute != "" {
				_ = cmd.Flags().Set("attribute", tt.attribute)
			}
			err := cmd.RunE(cmd, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("RunE() error = %v, want it to contain %q", err, tt.wantErrText)
			}
		})
	}
}

// TestNewClusterSubscribeCmd_IntervalValidationRunsBeforeConnecting proves
// interval validation happens before any connection attempt: with a valid
// selector and resolved target, an inverted --min/--max still returns the
// interval error rather than attempting connectToNode (which would need a
// real store/device and fail with a different, non-deterministic error).
func TestNewClusterSubscribeCmd_IntervalValidationRunsBeforeConnecting(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 1, Endpoint: 1, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	cmd := newClusterSubscribeCmd()
	_ = cmd.Flags().Set("cluster", "OnOff")
	_ = cmd.Flags().Set("attribute", "OnOff")
	_ = cmd.Flags().Set("min", "30")
	_ = cmd.Flags().Set("max", "10")

	err := cmd.RunE(cmd, nil)
	wantErrText := "--max (10s) must be >= --min (30s)"
	if err == nil || !strings.Contains(err.Error(), wantErrText) {
		t.Errorf("RunE() error = %v, want it to contain %q", err, wantErrText)
	}
}

// ---------------------------------------------------------------------------
// Command-level tests: shorthand `<Cluster> subscribe <attribute>`
//
// registerShorthandClusters() builds these per-cluster inside init(), so
// there's no standalone constructor to call fresh the way
// newClusterSubscribeCmd() allows for the generic form. These tests locate
// the already-registered "OnOff subscribe" command and drive it directly,
// restoring any flag they change afterward since it's shared, live state.
// ---------------------------------------------------------------------------

// findShorthandSubcommand locates the named child command (e.g. "subscribe")
// under the named shorthand cluster root (e.g. "OnOff") from the real,
// already-registered shorthandCmds tree.
func findShorthandSubcommand(t *testing.T, clusterName, childName string) *cobra.Command {
	t.Helper()
	for _, c := range shorthandCmds {
		if c.Name() != clusterName {
			continue
		}
		for _, child := range c.Commands() {
			if child.Name() == childName {
				return child
			}
		}
		t.Fatalf("shorthand cluster %q has no %q subcommand", clusterName, childName)
	}
	t.Fatalf("shorthand cluster %q not registered", clusterName)
	return nil
}

func TestShorthandSubscribeCmd_FlagsAndAnnotation(t *testing.T) {
	cmd := findShorthandSubcommand(t, "OnOff", "subscribe")

	for _, want := range []struct{ name, shorthand string }{
		{"min", "m"}, {"max", "M"}, {"count", "n"}, {"duration", "d"},
	} {
		f := cmd.Flags().Lookup(want.name)
		if f == nil {
			t.Errorf("flag %q not registered", want.name)
			continue
		}
		if f.Shorthand != want.shorthand {
			t.Errorf("flag %q shorthand = %q, want %q", want.name, f.Shorthand, want.shorthand)
		}
	}
	if cmd.Annotations[bypassDaemonAnnotation] != "true" {
		t.Error("shorthand subscribe command must carry bypassDaemonAnnotation so -K never spawns a daemon for it")
	}
}

// TestShorthandSubscribeCmd_Completion verifies shorthand attribute
// completion uses the same search-and-match behavior as shorthand attribute
// reads (clusters.Global.SearchAttributes), per issue #77's requirement that
// subscribe completion match read's.
func TestShorthandSubscribeCmd_Completion(t *testing.T) {
	subscribeCmd := findShorthandSubcommand(t, "OnOff", "subscribe")
	readCmd := findShorthandSubcommand(t, "OnOff", "read")

	gotSubscribe, subscribeDirective := subscribeCmd.ValidArgsFunction(subscribeCmd, nil, "on")
	gotRead, readDirective := readCmd.ValidArgsFunction(readCmd, nil, "on")

	if subscribeDirective != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %v, want ShellCompDirectiveNoFileComp", subscribeDirective)
	}
	if readDirective != subscribeDirective {
		t.Errorf("subscribe completion directive %v differs from read's %v", subscribeDirective, readDirective)
	}
	if len(gotSubscribe) == 0 {
		t.Fatalf(`completion for "on" = %v, want at least the OnOff attribute`, gotSubscribe)
	}
	if len(gotSubscribe) != len(gotRead) {
		t.Errorf("subscribe completion %v differs in shape from read's %v", gotSubscribe, gotRead)
	}

	// Once an attribute has already been given, no further completion is offered.
	gotWithArg, _ := subscribeCmd.ValidArgsFunction(subscribeCmd, []string{"OnOff"}, "")
	if len(gotWithArg) != 0 {
		t.Errorf("completion with an attribute already given = %v, want none", gotWithArg)
	}
}

func TestShorthandSubscribeCmd_UnknownAttribute(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 1, Endpoint: 1, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	cmd := findShorthandSubcommand(t, "OnOff", "subscribe")
	err := cmd.RunE(cmd, []string{"NotAnAttribute"})
	wantErrText := `unknown attribute "NotAnAttribute"`
	if err == nil || !strings.Contains(err.Error(), wantErrText) {
		t.Errorf("RunE() error = %v, want it to contain %q", err, wantErrText)
	}
}

func TestShorthandSubscribeCmd_IntervalValidationRunsBeforeConnecting(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 1, Endpoint: 1, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	cmd := findShorthandSubcommand(t, "OnOff", "subscribe")
	_ = cmd.Flags().Set("min", "30")
	_ = cmd.Flags().Set("max", "10")
	t.Cleanup(func() {
		_ = cmd.Flags().Set("min", cmd.Flags().Lookup("min").DefValue)
		_ = cmd.Flags().Set("max", cmd.Flags().Lookup("max").DefValue)
	})

	err := cmd.RunE(cmd, []string{"OnOff"})
	wantErrText := "--max (10s) must be >= --min (30s)"
	if err == nil || !strings.Contains(err.Error(), wantErrText) {
		t.Errorf("RunE() error = %v, want it to contain %q", err, wantErrText)
	}
}
