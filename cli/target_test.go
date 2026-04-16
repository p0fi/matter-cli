// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/store"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantNodeID      uint64
		wantEP          uint16
		wantEPSet       bool
		wantExplicit    bool
		wantErr         bool
		errContains     string
	}{
		{
			name:         "numeric node only",
			input:        "@1",
			wantNodeID:   1,
			wantEPSet:    false,
			wantExplicit: false,
		},
		{
			name:         "numeric node with endpoint",
			input:        "@1/2",
			wantNodeID:   1,
			wantEP:       2,
			wantEPSet:    true,
			wantExplicit: true,
		},
		{
			name:         "numeric node with endpoint 0",
			input:        "@1/0",
			wantNodeID:   1,
			wantEP:       0,
			wantEPSet:    true,
			wantExplicit: true,
		},
		{
			name:         "large node ID",
			input:        "@12345",
			wantNodeID:   12345,
			wantEPSet:    false,
			wantExplicit: false,
		},
		{
			name:         "large node ID with endpoint",
			input:        "@999/15",
			wantNodeID:   999,
			wantEP:       15,
			wantEPSet:    true,
			wantExplicit: true,
		},
		{
			name:        "missing @ prefix",
			input:       "1/2",
			wantErr:     true,
			errContains: "must start with @",
		},
		{
			name:        "empty after @",
			input:       "@",
			wantErr:     true,
			errContains: "empty target",
		},
		{
			name:        "node ID 0 is reserved",
			input:       "@0",
			wantErr:     true,
			errContains: "node ID 0 is reserved",
		},
		{
			name:        "node ID 0 with endpoint",
			input:       "@0/1",
			wantErr:     true,
			errContains: "node ID 0 is reserved",
		},
		{
			name:        "invalid endpoint not a number",
			input:       "@1/abc",
			wantErr:     true,
			errContains: "invalid endpoint",
		},
		{
			name:        "endpoint too large",
			input:       "@1/99999",
			wantErr:     true,
			errContains: "invalid endpoint",
		},
		{
			name:        "alias without store fails",
			input:       "@kitchen",
			wantErr:     true,
			errContains: "resolving alias",
		},
		{
			name:        "alias with endpoint without store fails",
			input:       "@kitchen/1",
			wantErr:     true,
			errContains: "resolving alias",
		},
		{
			name:        "empty node with slash",
			input:       "@/1",
			wantErr:     true,
			errContains: "resolving alias",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTarget(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTarget(%q) = %+v, want error containing %q", tt.input, got, tt.errContains)
				}
				if tt.errContains != "" && !containsStr(err.Error(), tt.errContains) {
					t.Fatalf("ParseTarget(%q) error = %q, want it to contain %q", tt.input, err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTarget(%q) unexpected error: %v", tt.input, err)
			}
			if got.NodeID != tt.wantNodeID {
				t.Errorf("ParseTarget(%q).NodeID = %d, want %d", tt.input, got.NodeID, tt.wantNodeID)
			}
			if got.Endpoint != tt.wantEP {
				t.Errorf("ParseTarget(%q).Endpoint = %d, want %d", tt.input, got.Endpoint, tt.wantEP)
			}
			if got.EndpointSet != tt.wantEPSet {
				t.Errorf("ParseTarget(%q).EndpointSet = %v, want %v", tt.input, got.EndpointSet, tt.wantEPSet)
			}
			if got.ExplicitEndpoint != tt.wantExplicit {
				t.Errorf("ParseTarget(%q).ExplicitEndpoint = %v, want %v", tt.input, got.ExplicitEndpoint, tt.wantExplicit)
			}
		})
	}
}

func TestIsTargetArg(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"@1", true},
		{"@1/2", true},
		{"@kitchen", true},
		{"@kitchen/1", true},
		{"@", false},       // too short — just "@" with nothing after
		{"1", false},       // no @ prefix
		{"--node", false},  // flag, not target
		{"on-off", false},  // command name
		{"", false},        // empty
		{"@@", true},       // weird but has @ prefix and len > 1
		{"@0", true},       // looks like a target (validation happens in ParseTarget)
		{"@123/456", true}, // multi-digit
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsTargetArg(tt.input)
			if got != tt.want {
				t.Errorf("IsTargetArg(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractTargetFromArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantArgs   []string
		wantTarget bool
		wantNodeID uint64
		wantEP     uint16
		wantEPSet  bool
	}{
		{
			name:       "target at beginning",
			args:       []string{"@1/2", "on-off", "toggle"},
			wantArgs:   []string{"on-off", "toggle"},
			wantTarget: true,
			wantNodeID: 1,
			wantEP:     2,
			wantEPSet:  true,
		},
		{
			name:       "target in middle",
			args:       []string{"on-off", "@1/2", "toggle"},
			wantArgs:   []string{"on-off", "toggle"},
			wantTarget: true,
			wantNodeID: 1,
			wantEP:     2,
			wantEPSet:  true,
		},
		{
			name:       "target at end",
			args:       []string{"on-off", "toggle", "@1/2"},
			wantArgs:   []string{"on-off", "toggle"},
			wantTarget: true,
			wantNodeID: 1,
			wantEP:     2,
			wantEPSet:  true,
		},
		{
			name:       "target without endpoint",
			args:       []string{"@5", "on-off", "toggle"},
			wantArgs:   []string{"on-off", "toggle"},
			wantTarget: true,
			wantNodeID: 5,
			wantEPSet:  false,
		},
		{
			name:       "no target",
			args:       []string{"on-off", "toggle", "--node", "1"},
			wantArgs:   []string{"on-off", "toggle", "--node", "1"},
			wantTarget: false,
		},
		{
			name:       "empty args",
			args:       []string{},
			wantArgs:   []string{},
			wantTarget: false,
		},
		{
			name:       "target after double dash is ignored",
			args:       []string{"on-off", "toggle", "--", "@1/2"},
			wantArgs:   []string{"on-off", "toggle", "--", "@1/2"},
			wantTarget: false,
		},
		{
			name:       "only first target is extracted",
			args:       []string{"@1/1", "on-off", "@2/2"},
			wantArgs:   []string{"on-off", "@2/2"},
			wantTarget: true,
			wantNodeID: 1,
			wantEP:     1,
			wantEPSet:  true,
		},
		{
			name:       "unparseable target left in args",
			args:       []string{"@kitchen", "on-off", "toggle"},
			wantArgs:   []string{"@kitchen", "on-off", "toggle"},
			wantTarget: false,
			// @kitchen requires store resolution which will fail in tests,
			// so the token is left in args for cobra to handle.
		},
		{
			name:       "bare @ is not a target",
			args:       []string{"@", "on-off", "toggle"},
			wantArgs:   []string{"@", "on-off", "toggle"},
			wantTarget: false,
		},
		{
			name:       "target is the only arg",
			args:       []string{"@3/1"},
			wantArgs:   []string{},
			wantTarget: true,
			wantNodeID: 3,
			wantEP:     1,
			wantEPSet:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying the test table.
			argsCopy := make([]string, len(tt.args))
			copy(argsCopy, tt.args)

			gotArgs, gotTarget := ExtractTargetFromArgs(argsCopy)

			// Check args.
			if len(gotArgs) != len(tt.wantArgs) {
				t.Fatalf("ExtractTargetFromArgs() args = %v (len %d), want %v (len %d)",
					gotArgs, len(gotArgs), tt.wantArgs, len(tt.wantArgs))
			}
			for i := range gotArgs {
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("ExtractTargetFromArgs() args[%d] = %q, want %q", i, gotArgs[i], tt.wantArgs[i])
				}
			}

			// Check target.
			if tt.wantTarget {
				if gotTarget == nil {
					t.Fatal("ExtractTargetFromArgs() target = nil, want non-nil")
				}
				if gotTarget.NodeID != tt.wantNodeID {
					t.Errorf("ExtractTargetFromArgs() target.NodeID = %d, want %d", gotTarget.NodeID, tt.wantNodeID)
				}
				if gotTarget.Endpoint != tt.wantEP {
					t.Errorf("ExtractTargetFromArgs() target.Endpoint = %d, want %d", gotTarget.Endpoint, tt.wantEP)
				}
				if gotTarget.EndpointSet != tt.wantEPSet {
					t.Errorf("ExtractTargetFromArgs() target.EndpointSet = %v, want %v", gotTarget.EndpointSet, tt.wantEPSet)
				}
			} else {
				if gotTarget != nil {
					t.Errorf("ExtractTargetFromArgs() target = %+v, want nil", gotTarget)
				}
			}
		})
	}
}

// testClusterRegistry returns a small registry for use in normalization tests.
func testClusterRegistry() *clusters.Registry {
	r := clusters.NewRegistry()
	r.Register(clusters.ClusterInfo{
		ID:          0x0006,
		Name:        "OnOff",
		DisplayName: "On/Off",
		Commands: []clusters.CommandInfo{
			{ID: 0, Name: "Off", DisplayName: "Off"},
			{ID: 1, Name: "On", DisplayName: "On"},
			{ID: 2, Name: "Toggle", DisplayName: "Toggle"},
		},
	})
	r.Register(clusters.ClusterInfo{
		ID:          0x0008,
		Name:        "LevelControl",
		DisplayName: "Level Control",
		Commands: []clusters.CommandInfo{
			{ID: 0, Name: "MoveToLevel", DisplayName: "Move To Level"},
		},
	})
	return r
}

func TestNormalizeShorthandArgs(t *testing.T) {
	reg := testClusterRegistry()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "lowercase cluster and command",
			in:   []string{"onoff", "on"},
			want: []string{"OnOff", "On"},
		},
		{
			name: "mixed case cluster and command",
			in:   []string{"OnOff", "toggle"},
			want: []string{"OnOff", "Toggle"},
		},
		{
			name: "all uppercase cluster",
			in:   []string{"ONOFF", "OFF"},
			want: []string{"OnOff", "Off"},
		},
		{
			name: "with leading flag",
			in:   []string{"--verbose", "onoff", "on"},
			want: []string{"--verbose", "OnOff", "On"},
		},
		{
			name: "unrecognised cluster unchanged",
			in:   []string{"cluster", "invoke", "--cluster", "on-off"},
			want: []string{"cluster", "invoke", "--cluster", "on-off"},
		},
		{
			name: "cluster only, no command",
			in:   []string{"onoff"},
			want: []string{"OnOff"},
		},
		{
			name: "unrecognised command token left unchanged",
			in:   []string{"onoff", "read", "OnOff"},
			want: []string{"OnOff", "read", "OnOff"},
		},
		{
			name: "multi-word cluster",
			in:   []string{"levelcontrol", "movetolevel"},
			want: []string{"LevelControl", "MoveToLevel"},
		},
		{
			name: "empty args",
			in:   []string{},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeShorthandArgs(tt.in, reg)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, len(want)=%d: got=%v want=%v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeShorthandArgs_DoesNotMutateInput(t *testing.T) {
	reg := testClusterRegistry()
	original := []string{"onoff", "toggle"}
	input := make([]string, len(original))
	copy(input, original)

	_ = normalizeShorthandArgs(input, reg)

	for i := range original {
		if input[i] != original[i] {
			t.Errorf("normalizeShorthandArgs mutated input[%d]: got %q, want %q",
				i, input[i], original[i])
		}
	}
}

func TestExtractTargetFromArgs_DoesNotMutateInput(t *testing.T) {
	original := []string{"@1/2", "on-off", "toggle"}
	input := make([]string, len(original))
	copy(input, original)

	_, _ = ExtractTargetFromArgs(input)

	for i := range original {
		if input[i] != original[i] {
			t.Errorf("ExtractTargetFromArgs mutated input[%d]: got %q, want %q",
				i, input[i], original[i])
		}
	}
}

func TestIsPastDoubleDash(t *testing.T) {
	tests := []struct {
		name string
		args []string
		idx  int
		want bool
	}{
		{
			name: "before double dash",
			args: []string{"on-off", "--", "@1"},
			idx:  0,
			want: false,
		},
		{
			name: "at double dash",
			args: []string{"on-off", "--", "@1"},
			idx:  1,
			want: false,
		},
		{
			name: "after double dash",
			args: []string{"on-off", "--", "@1"},
			idx:  2,
			want: true,
		},
		{
			name: "no double dash",
			args: []string{"on-off", "@1", "toggle"},
			idx:  1,
			want: false,
		},
		{
			name: "empty args",
			args: []string{},
			idx:  0,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPastDoubleDash(tt.args, tt.idx)
			if got != tt.want {
				t.Errorf("isPastDoubleDash(%v, %d) = %v, want %v", tt.args, tt.idx, got, tt.want)
			}
		})
	}
}

func TestRequireTarget_NoTarget(t *testing.T) {
	// Ensure no resolved target is set for this test.
	prev := resolvedTarget
	resolvedTarget = nil
	defer func() { resolvedTarget = prev }()

	_, _, err := requireTarget(nil)
	if err == nil {
		t.Fatal("requireTarget() with no target should return error")
	}
	// Verify the error message mentions all the methods.
	errMsg := err.Error()
	wantPhrases := []string{
		"no target specified",
		"@1/1",
		"@kitchen",
		"matter use",
		"MATTER_TARGET",
	}
	for _, phrase := range wantPhrases {
		if !containsStr(errMsg, phrase) {
			t.Errorf("requireTarget() error should mention %q, got:\n%s", phrase, errMsg)
		}
	}
}

func TestRequireTarget_WithNode(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 42, Endpoint: 3, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	nodeID, endpoint, err := requireTarget(nil)
	if err != nil {
		t.Fatalf("requireTarget() unexpected error: %v", err)
	}
	if nodeID != 42 {
		t.Errorf("requireTarget() nodeID = %d, want 42", nodeID)
	}
	if endpoint != 3 {
		t.Errorf("requireTarget() endpoint = %d, want 3", endpoint)
	}
}

func TestTargetHint(t *testing.T) {
	tests := []struct {
		nodeID   uint64
		endpoint uint16
		want     string
	}{
		{1, 1, "@1/1"},
		{42, 0, "@42/0"},
		{999, 15, "@999/15"},
	}

	for _, tt := range tests {
		got := targetHint(tt.nodeID, tt.endpoint)
		if got != tt.want {
			t.Errorf("targetHint(%d, %d) = %q, want %q", tt.nodeID, tt.endpoint, got, tt.want)
		}
	}
}

func TestInferDefaultEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []store.Endpoint
		wantEP    uint16
		wantOK    bool
	}{
		{
			name:      "no endpoints",
			endpoints: nil,
			wantOK:    false,
		},
		{
			name:      "only root endpoint",
			endpoints: []store.Endpoint{{ID: 0}},
			wantOK:    false,
		},
		{
			name:      "root and application endpoint",
			endpoints: []store.Endpoint{{ID: 0}, {ID: 1}},
			wantEP:    1,
			wantOK:    true,
		},
		{
			name:      "multiple non-root endpoints returns first",
			endpoints: []store.Endpoint{{ID: 0}, {ID: 2}, {ID: 3}},
			wantEP:    2,
			wantOK:    true,
		},
		{
			name:      "only non-root endpoint",
			endpoints: []store.Endpoint{{ID: 5}},
			wantEP:    5,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &store.Node{Endpoints: tt.endpoints}
			gotEP, gotOK := inferDefaultEndpoint(node)
			if gotOK != tt.wantOK {
				t.Errorf("inferDefaultEndpoint() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotEP != tt.wantEP {
				t.Errorf("inferDefaultEndpoint() endpoint = %d, want %d", gotEP, tt.wantEP)
			}
		})
	}
}

// containsStr is a helper that reports whether s contains substr.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
