// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

func TestParseRenameArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantName     string
		wantTargetID uint64
		wantErr      bool
	}{
		{name: "target and name", args: []string{"@1", "Kitchen"}, wantName: "Kitchen", wantTargetID: 1},
		{name: "name only with inline target", args: []string{"Kitchen"}, wantName: "Kitchen"},
		{name: "target only for reset", args: []string{"@42"}, wantTargetID: 42},
		{name: "no args for reset with inline target", args: nil},
		{name: "invalid target", args: []string{"@notanum", "x"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolvedTarget = nil
			got, err := parseRenameArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantName {
				t.Errorf("name = %q, want %q", got, tc.wantName)
			}
			if tc.wantTargetID != 0 {
				if resolvedTarget == nil || resolvedTarget.NodeID != tc.wantTargetID {
					t.Errorf("resolvedTarget = %+v, want NodeID=%d", resolvedTarget, tc.wantTargetID)
				}
			}
		})
	}
}

func TestValidateNodeLabel(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		wantErr string // substring the error should contain; empty means expect no error
	}{
		{name: "normal", label: "Kitchen Light"},
		{name: "exactly 32 bytes", label: strings.Repeat("a", 32)},
		{name: "empty", label: "", wantErr: "required"},
		{name: "only whitespace", label: "   ", wantErr: "required"},
		{name: "trailing space", label: "Kitchen ", wantErr: "whitespace"},
		{name: "leading space", label: " Kitchen", wantErr: "whitespace"},
		{name: "33 bytes", label: strings.Repeat("a", 33), wantErr: "32-byte"},
		{name: "multibyte within limit", label: "日本語 lamp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateNodeLabel(tc.label)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestDisplayOldName(t *testing.T) {
	if got := displayOldName(""); got != "(unnamed)" {
		t.Errorf(`displayOldName("") = %q, want "(unnamed)"`, got)
	}
	if got := displayOldName("Kitchen"); got != "Kitchen" {
		t.Errorf(`displayOldName("Kitchen") = %q, want "Kitchen"`, got)
	}
}

func TestDecodeTLVString(t *testing.T) {
	t.Run("valid string", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutUTF8String(tlv.AnonymousTag(), "Kitchen Light"); err != nil {
			t.Fatal(err)
		}
		got, err := decodeTLVString(w.Bytes())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "Kitchen Light" {
			t.Errorf("got %q, want %q", got, "Kitchen Light")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		_, err := decodeTLVString(nil)
		if err == nil {
			t.Error("expected error for empty input")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		w := tlv.NewWriter()
		if err := w.PutUnsignedInt(tlv.AnonymousTag(), 42); err != nil {
			t.Fatal(err)
		}
		_, err := decodeTLVString(w.Bytes())
		if err == nil {
			t.Error("expected error for non-string TLV")
		}
	})
}
