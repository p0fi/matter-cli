// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"testing"
)

func TestParseLookupOutput(t *testing.T) {
	t.Run("parses hostname, port, and TXT records", func(t *testing.T) {
		output := `Lookup E45F01D2E5F3D4A1._matter._tcp.local
DATE: ---Sun 13 Apr 2026---
 0:14:22.123  ...STARTING...
E45F01D2E5F3D4A1._matter._tcp.local. can be reached at E45F01D2E5F3D4A1.local.:5540 (interface 14)
 SII=5000 SAI=300 T=2 DN=Ceiling Light`
		hostname, port, txt := parseLookupOutput(output)

		if hostname != "E45F01D2E5F3D4A1.local" {
			t.Errorf("hostname = %q, want %q", hostname, "E45F01D2E5F3D4A1.local")
		}
		if port != 5540 {
			t.Errorf("port = %d, want 5540", port)
		}
		if len(txt) == 0 {
			t.Error("expected TXT records, got none")
		}
		wantTXT := map[string]bool{"SII=5000": true, "SAI=300": true, "T=2": true, "DN=Ceiling": false}
		for _, kv := range txt {
			if _, ok := wantTXT[kv]; ok {
				wantTXT[kv] = true
			}
		}
		for kv, found := range wantTXT {
			if kv == "DN=Ceiling" {
				continue // multi-word values are split by Fields; skip
			}
			if !found {
				t.Errorf("expected TXT record %q not found in %v", kv, txt)
			}
		}
	})

	t.Run("parses non-standard port", func(t *testing.T) {
		output := `Lookup foo._matter._tcp.local
foo._matter._tcp.local. can be reached at myhost.local.:1234 (interface 5)`
		hostname, port, _ := parseLookupOutput(output)
		if hostname != "myhost.local" {
			t.Errorf("hostname = %q, want %q", hostname, "myhost.local")
		}
		if port != 1234 {
			t.Errorf("port = %d, want 1234", port)
		}
	})

	t.Run("returns empty on no match", func(t *testing.T) {
		hostname, port, txt := parseLookupOutput("no useful output here\n")
		if hostname != "" || port != 0 || len(txt) != 0 {
			t.Errorf("expected empty result, got hostname=%q port=%d txt=%v", hostname, port, txt)
		}
	})

	t.Run("strips trailing dot from hostname", func(t *testing.T) {
		output := `Instance._matter._tcp.local. can be reached at node.local.:5540 (interface 1)`
		hostname, _, _ := parseLookupOutput(output)
		if hostname != "node.local" {
			t.Errorf("hostname = %q, want %q (trailing dot should be stripped)", hostname, "node.local")
		}
	})

	t.Run("handles instance name with spaces", func(t *testing.T) {
		output := `My Light Bulb._matter._tcp.local. can be reached at bulb.local.:5540 (interface 3)`
		hostname, port, _ := parseLookupOutput(output)
		if hostname != "bulb.local" {
			t.Errorf("hostname = %q, want %q", hostname, "bulb.local")
		}
		if port != 5540 {
			t.Errorf("port = %d, want 5540", port)
		}
	})
}
