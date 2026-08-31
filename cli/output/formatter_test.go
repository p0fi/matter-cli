// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import "testing"

// TestResolveFormat pins the documented default. Tests run with stdout piped,
// so the empty and unrecognised cases resolve to JSON here — the branch that
// matters for a command deciding where to put its progress output.
func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   FormatType
	}{
		{"explicit table", "table", FormatTable},
		{"explicit json", "json", FormatJSON},
		{"explicit yaml", "yaml", FormatYAML},
		{"empty defaults to json when piped", "", FormatJSON},
		{"unrecognised defaults to json when piped", "toml", FormatJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveFormat(tt.format); got != tt.want {
				t.Errorf("ResolveFormat(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

// TestNewMatchesResolveFormat keeps the formatter selection and the resolved
// format in step, so a command that branches on ResolveFormat cannot end up
// rendering through a formatter of a different kind.
func TestNewMatchesResolveFormat(t *testing.T) {
	for _, format := range []string{"", "table", "json", "yaml", "toml"} {
		f := New(format)
		switch ResolveFormat(format) {
		case FormatTable:
			if _, ok := f.(*TableFormatter); !ok {
				t.Errorf("New(%q) = %T, want *TableFormatter", format, f)
			}
		default:
			if _, ok := f.(*JSONFormatter); !ok {
				t.Errorf("New(%q) = %T, want *JSONFormatter", format, f)
			}
		}
	}
}
