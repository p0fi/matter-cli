// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"strings"
	"testing"
)

// TestTipIsDistinctFromStepperOutput pins the property the tip exists for: a
// hint must not be mistakable for a line the stepper printed. Stepper lines
// start at column 0 with one of ● ✓ ✗ ▲; a tip is indented and badged.
func TestTipIsDistinctFromStepperOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	tip := Tip("Run something")

	if !strings.HasPrefix(tip, "  ") {
		t.Errorf("Tip should be indented, got %q", tip)
	}
	for _, icon := range []string{"●", "✓", "✗", "▲", "◉"} {
		if strings.Contains(tip, icon) {
			t.Errorf("Tip must not borrow the stepper icon %q: %q", icon, tip)
		}
	}
	if !strings.Contains(tip, "TIP") {
		t.Errorf("Tip should carry a TIP label, got %q", tip)
	}
}

func TestTipBadge(t *testing.T) {
	t.Run("NO_COLOR degrades to a plain label", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if got := TipBadge(); got != "TIP:" {
			t.Errorf("TipBadge() = %q, want %q", got, "TIP:")
		}
	})

	t.Run("NO_COLOR tip has no ragged padding", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		if got, want := Tip("Run something"), "  TIP: Run something"; got != want {
			t.Errorf("Tip() = %q, want %q", got, want)
		}
	})
}

// TestTipEmitsBodyVerbatim covers the documented contract that callers style
// the body themselves, so a highlighted command inside a hint survives intact.
func TestTipEmitsBodyVerbatim(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	body := Muted("Run ") + Command("matter @1 cluster discover") + Muted(" to enable completion")
	tip := Tip(body)

	if !strings.HasSuffix(tip, body) {
		t.Errorf("Tip(%q) should end with its body verbatim, got %q", body, tip)
	}
	if !strings.Contains(tip, "matter @1 cluster discover") {
		t.Errorf("Tip lost the embedded command: %q", tip)
	}
}
