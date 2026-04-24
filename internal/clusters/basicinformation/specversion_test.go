// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package basicinformation

import "testing"

func TestFormatSpecVersion(t *testing.T) {
	tests := []struct {
		v    uint32
		want string
	}{
		{0x00000000, ""},
		{0x01030000, "1.3"},
		{0x01040000, "1.4"},
		{0x01000000, "1.0"},
		{0x01030100, "1.3.1"},
		{0x02010200, "2.1.2"},
	}
	for _, tt := range tests {
		got := FormatSpecVersion(tt.v)
		if got != tt.want {
			t.Errorf("FormatSpecVersion(0x%08X) = %q, want %q", tt.v, got, tt.want)
		}
	}
}
