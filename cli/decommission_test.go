// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

// buildNOCResponseFields encodes a bare NOCResponse body (rawstruct — no outer
// Structure wrapper) as a helper for TestDecodeNOCResponse.
func buildNOCResponseFields(t *testing.T, status uint8, fabricIndex uint8, debug string, withDebug bool) []byte {
	t.Helper()
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.ContextTag(0), uint64(status)); err != nil {
		t.Fatalf("encode status: %v", err)
	}
	if err := w.PutUnsignedInt(tlv.ContextTag(1), uint64(fabricIndex)); err != nil {
		t.Fatalf("encode fabric index: %v", err)
	}
	if withDebug {
		if err := w.PutUTF8String(tlv.ContextTag(2), debug); err != nil {
			t.Fatalf("encode debug: %v", err)
		}
	}
	return w.Bytes()
}

func TestDecodeNOCResponse(t *testing.T) {
	tests := []struct {
		name       string
		fields     func(t *testing.T) []byte
		wantStatus uint8
		wantDebug  string
		wantErr    bool
	}{
		{
			name: "ok response no debug",
			fields: func(t *testing.T) []byte {
				return buildNOCResponseFields(t, 0x00, 1, "", false)
			},
			wantStatus: 0x00,
			wantDebug:  "",
		},
		{
			name: "error with debug text",
			fields: func(t *testing.T) []byte {
				return buildNOCResponseFields(t, 0x0B, 2, "invalid fabric index", true)
			},
			wantStatus: 0x0B,
			wantDebug:  "invalid fabric index",
		},
		{
			name: "empty input errors",
			fields: func(t *testing.T) []byte {
				return nil
			},
			wantErr: true,
		},
		{
			name: "unknown tags are ignored",
			fields: func(t *testing.T) []byte {
				w := tlv.NewWriter()
				_ = w.PutUnsignedInt(tlv.ContextTag(0), 0)
				_ = w.PutUnsignedInt(tlv.ContextTag(1), 3)
				_ = w.PutUnsignedInt(tlv.ContextTag(9), 42)
				return w.Bytes()
			},
			wantStatus: 0,
			wantDebug:  "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, debug, err := decodeNOCResponse(tc.fields(t))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if status != tc.wantStatus {
				t.Errorf("status = 0x%02X, want 0x%02X", status, tc.wantStatus)
			}
			if debug != tc.wantDebug {
				t.Errorf("debug = %q, want %q", debug, tc.wantDebug)
			}
		})
	}
}

func TestParseNOCResponse(t *testing.T) {
	t.Run("ok returns nil", func(t *testing.T) {
		fields := buildNOCResponseFields(t, nocStatusOK, 1, "", false)
		if err := parseNOCResponse(fields); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("invalid fabric index includes debug text", func(t *testing.T) {
		fields := buildNOCResponseFields(t, nocStatusInvalidFabricIndex, 0, "no such fabric", true)
		err := parseNOCResponse(fields)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "InvalidFabricIndex") {
			t.Errorf("missing status name in error: %v", err)
		}
		if !contains(err.Error(), "no such fabric") {
			t.Errorf("missing debug text in error: %v", err)
		}
	})

	t.Run("unknown status code", func(t *testing.T) {
		fields := buildNOCResponseFields(t, 0x77, 1, "", false)
		err := parseNOCResponse(fields)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !contains(err.Error(), "0x77") {
			t.Errorf("error should mention hex status: %v", err)
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
