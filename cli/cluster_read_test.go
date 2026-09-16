// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/p0fi/matter-cli/internal/clusters"
	"github.com/p0fi/matter-cli/internal/interaction"
	"github.com/p0fi/matter-cli/internal/tlv"
)

// tlvUint encodes a bare unsigned integer as an attribute payload would carry it.
func tlvUint(t *testing.T, v uint64) []byte {
	t.Helper()
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.AnonymousTag(), v); err != nil {
		t.Fatal(err)
	}
	return w.Bytes()
}

// tlvBool encodes a bare boolean attribute payload.
func tlvBool(t *testing.T, v bool) []byte {
	t.Helper()
	w := tlv.NewWriter()
	if err := w.PutBool(tlv.AnonymousTag(), v); err != nil {
		t.Fatal(err)
	}
	return w.Bytes()
}

// tlvString encodes a bare UTF-8 string attribute payload.
func tlvString(t *testing.T, v string) []byte {
	t.Helper()
	w := tlv.NewWriter()
	if err := w.PutUTF8String(tlv.AnonymousTag(), v); err != nil {
		t.Fatal(err)
	}
	return w.Bytes()
}

// testReadTarget is the addressee every record-building test reads against.
func testReadTarget(t *testing.T) readTarget {
	t.Helper()
	return readTarget{nodeID: 1, endpoint: 1, cl: onOffCluster(t)}
}

// TestBuildReadRecords_Ordering proves the records come back sorted by
// attribute ID regardless of the order the device reported them, and that
// ascending order is what puts the global attributes at the bottom.
func TestBuildReadRecords_Ordering(t *testing.T) {
	reports := []attrReport{
		{attributeID: 0xFFFD, data: tlvUint(t, 6)},
		{attributeID: 0x4001, data: tlvUint(t, 30)},
		{attributeID: 0xFFFC, data: tlvUint(t, 1)},
		{attributeID: 0x0000, data: tlvBool(t, true)},
	}

	records := buildReadRecords(testReadTarget(t), reports, time.Now())

	want := []uint32{0x0000, 0x4001, 0xFFFC, 0xFFFD}
	if len(records) != len(want) {
		t.Fatalf("got %d records, want %d", len(records), len(want))
	}
	for i, id := range want {
		if records[i].AttributeID != id {
			t.Errorf("record %d attribute ID = 0x%04X, want 0x%04X", i, records[i].AttributeID, id)
		}
	}
}

// TestBuildReadRecords_NameResolution covers the three-step registry → global
// attribute table → hex fallback that decides how an attribute is labelled.
func TestBuildReadRecords_NameResolution(t *testing.T) {
	tests := []struct {
		name        string
		attributeID uint32
		wantName    string
	}{
		{"registry-known attribute", 0x0000, "OnOff"},
		{"global attribute", 0xFFFB, "AttributeList"},
		{"unknown vendor attribute falls back to hex", 0x1234, "0x1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := buildReadRecords(testReadTarget(t),
				[]attrReport{{attributeID: tt.attributeID, data: tlvUint(t, 1)}}, time.Now())

			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}
			if records[0].Attribute != tt.wantName {
				t.Errorf("attribute name = %q, want %q", records[0].Attribute, tt.wantName)
			}
		})
	}
}

// TestBuildReadRecords_StatusReports proves an attribute the device refuses to
// disclose becomes a visible row carrying an error and no value, rather than
// disappearing from the output or aborting the read.
func TestBuildReadRecords_StatusReports(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantDisplay string
	}{
		{"unsupported access", interaction.NewStatusError(uint8(interaction.StatusUnsupportedAccess), nil), "<access denied>"},
		{"timeout", context.DeadlineExceeded, "<timeout>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records := buildReadRecords(testReadTarget(t),
				[]attrReport{{attributeID: 0x0000, err: tt.err}}, time.Now())

			if len(records) != 1 {
				t.Fatalf("got %d records, want 1", len(records))
			}
			rec := records[0]
			if rec.Value != nil {
				t.Errorf("value = %#v, want nil for a status-only report", rec.Value)
			}
			if rec.Error != tt.err.Error() {
				t.Errorf("error = %q, want the full status text %q", rec.Error, tt.err.Error())
			}
			if rec.Display != tt.wantDisplay {
				t.Errorf("display = %q, want %q", rec.Display, tt.wantDisplay)
			}
		})
	}
}

// TestBuildReadRecords_NativeValues checks that machine output carries real Go
// types — the same ones subscribe emits — rather than display strings.
func TestBuildReadRecords_NativeValues(t *testing.T) {
	arrayW := tlv.NewWriter()
	if err := arrayW.StartArray(tlv.AnonymousTag()); err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint64{0, 1} {
		if err := arrayW.PutUnsignedInt(tlv.AnonymousTag(), v); err != nil {
			t.Fatal(err)
		}
	}
	if err := arrayW.EndContainer(); err != nil {
		t.Fatal(err)
	}

	structW := tlv.NewWriter()
	if err := structW.StartStructure(tlv.AnonymousTag()); err != nil {
		t.Fatal(err)
	}
	if err := structW.PutUnsignedInt(tlv.ContextTag(1), 7); err != nil {
		t.Fatal(err)
	}
	if err := structW.EndContainer(); err != nil {
		t.Fatal(err)
	}

	records := buildReadRecords(testReadTarget(t), []attrReport{
		{attributeID: 0x0000, data: tlvBool(t, true)},
		{attributeID: 0x4001, data: tlvUint(t, 30)},
		{attributeID: 0xFFFB, data: arrayW.Bytes()},
		{attributeID: 0x1234, data: structW.Bytes()},
	}, time.Now())

	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}
	if v, ok := records[0].Value.(bool); !ok || !v {
		t.Errorf("OnOff value = %#v, want bool(true)", records[0].Value)
	}
	if v, ok := records[1].Value.(map[string]any); !ok || v["1"] != uint64(7) {
		t.Errorf("struct value = %#v, want map[1:7]", records[1].Value)
	}
	if v, ok := records[2].Value.(uint64); !ok || v != 30 {
		t.Errorf("OnTime value = %#v, want uint64(30)", records[2].Value)
	}
	if v, ok := records[3].Value.([]any); !ok || len(v) != 2 {
		t.Errorf("AttributeList value = %#v, want a 2-element []any", records[3].Value)
	}
}

// TestBuildReadRecords_BitmapDisplay guards the FeatureMap rendering the
// single-attribute human output depends on: the display string carries the
// binary breakdown while the native value stays a plain integer.
func TestBuildReadRecords_BitmapDisplay(t *testing.T) {
	records := buildReadRecords(testReadTarget(t),
		[]attrReport{{attributeID: featureMapAttrID, data: tlvUint(t, 5)}}, time.Now())

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if want := "5 (0b101)"; records[0].Display != want {
		t.Errorf("display = %q, want %q", records[0].Display, want)
	}
	if v, ok := records[0].Value.(uint64); !ok || v != 5 {
		t.Errorf("value = %#v, want uint64(5)", records[0].Value)
	}
}

// TestBuildReadRecords_TruncationIsDisplayOnly proves the 40-character
// middle-truncation that keeps table columns aligned never reaches the native
// value machine consumers read.
func TestBuildReadRecords_TruncationIsDisplayOnly(t *testing.T) {
	long := strings.Repeat("a", 80)
	records := buildReadRecords(testReadTarget(t),
		[]attrReport{{attributeID: 0x1234, data: tlvString(t, long)}}, time.Now())

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if len(records[0].Display) > maxValueLen {
		t.Errorf("display is %d chars, want at most %d", len(records[0].Display), maxValueLen)
	}
	if !strings.Contains(records[0].Display, "...") {
		t.Errorf("display = %q, want it middle-truncated", records[0].Display)
	}
	if records[0].Value != long {
		t.Errorf("value = %#v, want the untruncated string", records[0].Value)
	}
}

// TestBuildReadRecords_UndecodableValue checks that an attribute whose TLV
// cannot be parsed still produces a record — carrying the raw bytes and the
// decode failure — instead of vanishing from a cluster-wide read.
func TestBuildReadRecords_UndecodableValue(t *testing.T) {
	records := buildReadRecords(testReadTarget(t),
		[]attrReport{{attributeID: 0x1234, data: []byte{0x15, 0x24}}}, time.Now())

	if len(records) != 1 {
		t.Fatalf("got %d records, want 1", len(records))
	}
	if records[0].Value != nil {
		t.Errorf("value = %#v, want nil when decoding failed", records[0].Value)
	}
	if records[0].DecodeError == "" {
		t.Error("decode_error should describe the failure")
	}
	if records[0].Raw != "0x1524" {
		t.Errorf("raw = %q, want the hex payload", records[0].Raw)
	}
}

// TestReadPaths_WildcardAndSingle asserts the two transports agree on what an
// omitted attribute means: no attribute ID on the wire, and the additive
// wildcard flag over IPC.
func TestReadPaths_WildcardAndSingle(t *testing.T) {
	cl := onOffCluster(t)
	attr, ok := clusters.Global.AttributeByName(cl.ID, "OnOff")
	if !ok {
		t.Fatal("OnOff attribute is not registered")
	}

	t.Run("wildcard", func(t *testing.T) {
		req := daemonReadPath(1, cl, nil)
		if !req.WildcardAttribute {
			t.Error("daemon path should set the wildcard flag when no attribute is named")
		}
		path := directReadPath(1, cl, nil)
		if path.AttributeID != nil {
			t.Errorf("direct path attribute ID = %v, want nil (wildcard)", *path.AttributeID)
		}
		if path.EndpointID == nil || *path.EndpointID != 1 {
			t.Error("the endpoint must stay pinned to the target, never wildcarded")
		}
	})

	t.Run("single attribute", func(t *testing.T) {
		req := daemonReadPath(1, cl, attr)
		if req.WildcardAttribute {
			t.Error("daemon path should not set the wildcard flag for a named attribute")
		}
		if req.AttributeID != attr.ID {
			t.Errorf("daemon path attribute ID = 0x%04X, want 0x%04X", req.AttributeID, attr.ID)
		}
		path := directReadPath(1, cl, attr)
		if path.AttributeID == nil || *path.AttributeID != attr.ID {
			t.Errorf("direct path attribute ID = %v, want 0x%04X", path.AttributeID, attr.ID)
		}
	})
}

// ---------------------------------------------------------------------------
// Command-level tests: `cluster read` and the `<Cluster> read` shorthand.
//
// As with the subscribe tests, these drive RunE directly and only exercise
// branches that return before any network or store operation — except
// TestNewClusterReadCmd_OmittedAttributeReachesTransport, which isolates the
// store under a temp XDG_CONFIG_HOME so that reaching the transport is safe.
// ---------------------------------------------------------------------------

func TestNewClusterReadCmd_SelectorResolution(t *testing.T) {
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
		{"unknown cluster", "NotACluster", "OnOff", `unknown cluster "NotACluster"`},
		{"unknown attribute", "OnOff", "NotAnAttribute", `unknown attribute "NotAnAttribute"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newClusterReadCmd()
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

// TestNewClusterReadCmd_OmittedAttributeReachesTransport proves the
// "--attribute is required" gate is gone: with only --cluster given, the
// command now proceeds to the transport and fails there (on an empty store
// holding no such node) rather than rejecting the invocation up front.
func TestNewClusterReadCmd_OmittedAttributeReachesTransport(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 1, Endpoint: 0, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	cmd := newClusterReadCmd()
	// Execute() would install this; RunE is driven directly here.
	cmd.SetContext(context.Background())
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	_ = cmd.Flags().Set("cluster", "OnOff")

	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("RunE() unexpectedly succeeded against an empty store")
	}
	if strings.Contains(err.Error(), "--attribute") {
		t.Errorf("RunE() error = %v, want the attribute requirement to be gone", err)
	}
	if !strings.Contains(err.Error(), "looking up node 1") {
		t.Errorf("RunE() error = %v, want it to have reached the node lookup", err)
	}
}

// TestShorthandReadCmd_ArgCount pins the relaxed argument count: no attribute
// (the whole cluster) and one attribute are both accepted, two are not.
func TestShorthandReadCmd_ArgCount(t *testing.T) {
	cmd := findShorthandSubcommand(t, "OnOff", "read")

	for _, args := range [][]string{{}, {"OnOff"}} {
		if err := cmd.Args(cmd, args); err != nil {
			t.Errorf("Args(%v) = %v, want nil", args, err)
		}
	}
	if err := cmd.Args(cmd, []string{"OnOff", "extra"}); err == nil {
		t.Error("Args() accepted two arguments, want an error")
	}
}

// TestShorthandReadCmd_UnknownAttribute confirms naming an attribute the
// cluster does not have still fails, rather than silently degrading into a
// whole-cluster read.
func TestShorthandReadCmd_UnknownAttribute(t *testing.T) {
	prev := resolvedTarget
	resolvedTarget = &Target{NodeID: 1, Endpoint: 1, EndpointSet: true}
	defer func() { resolvedTarget = prev }()

	cmd := findShorthandSubcommand(t, "OnOff", "read")
	err := cmd.RunE(cmd, []string{"NotAnAttribute"})
	wantErrText := `unknown attribute "NotAnAttribute"`
	if err == nil || !strings.Contains(err.Error(), wantErrText) {
		t.Errorf("RunE() error = %v, want it to contain %q", err, wantErrText)
	}
}

// TestEmptyReadError names the cluster and endpoint the user targeted and
// points at tree, since a wrong endpoint is the overwhelmingly likely cause.
func TestEmptyReadError(t *testing.T) {
	err := emptyReadError(7, 2, onOffCluster(t))
	for _, want := range []string{"node 7", "endpoint 2", "On/Off", "matter @7 tree"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err.Error(), want)
		}
	}
}

// TestDerefAttrID keeps a wildcard echoed back by a device visible rather than
// panicking on the nil path component.
func TestDerefAttrID(t *testing.T) {
	if got := derefAttrID(nil); got != 0 {
		t.Errorf("derefAttrID(nil) = %d, want 0", got)
	}
	id := uint32(0x4001)
	if got := derefAttrID(&id); got != id {
		t.Errorf("derefAttrID(&0x4001) = 0x%04X, want 0x4001", got)
	}
}
