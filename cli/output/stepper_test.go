// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// newStaticStepper creates a non-verbose, non-animate, non-silent Stepper that
// writes to buf. This is the "piped" path and the easiest to test without TTY.
func newStaticStepper(buf *bytes.Buffer) *Stepper {
	return &Stepper{
		w:       buf,
		verbose: false,
		animate: false,
		silent:  false,
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0ms"},
		{5 * time.Millisecond, "5ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1.0s"},
		{1500 * time.Millisecond, "1.5s"},
		{59*time.Second + 900*time.Millisecond, "59.9s"},
		{time.Minute, "1m0s"},
		{time.Minute + 2*time.Second, "1m2s"},
		{2*time.Minute + 30*time.Second, "2m30s"},
	}

	for _, tt := range tests {
		t.Run(tt.d.String(), func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestStepper_StaticMode_StepDuration(t *testing.T) {
	var buf bytes.Buffer
	s := newStaticStepper(&buf)

	s.Step("Alpha")
	s.Step("Beta") // completes Alpha
	s.Success("Done")

	out := buf.String()

	// Each completed step and the success line should have a duration suffix.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 output lines, got %d: %q", len(lines), out)
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		// Duration should appear after two spaces.
		if !strings.Contains(line, "  ") {
			t.Errorf("line %q has no two-space separator before duration", line)
		}
		parts := strings.SplitN(line, "  ", 2)
		dur := strings.TrimSpace(parts[len(parts)-1])
		if dur == "" {
			t.Errorf("line %q has no duration suffix", line)
		}
		// Duration must end with "ms", "s", or match "XmYs".
		if !strings.HasSuffix(dur, "ms") && !strings.HasSuffix(dur, "s") {
			t.Errorf("duration %q has unexpected format", dur)
		}
	}
}

func TestStepper_StaticMode_SuccessLine(t *testing.T) {
	var buf bytes.Buffer
	s := newStaticStepper(&buf)

	s.Step("Connecting")
	s.Success("Connected")

	out := buf.String()
	if !strings.Contains(out, "Connected") {
		t.Errorf("output %q missing success message", out)
	}
	// Success line must contain a duration.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	successLine := lines[len(lines)-1]
	if !strings.Contains(successLine, "  ") {
		t.Errorf("success line %q missing duration suffix", successLine)
	}
}

func TestStepper_StaticMode_FailLine(t *testing.T) {
	var buf bytes.Buffer
	s := newStaticStepper(&buf)

	s.Step("Connecting")
	s.Fail("Connection refused")

	out := buf.String()
	if !strings.Contains(out, "Connection refused") {
		t.Errorf("output %q missing fail message", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	failLine := lines[len(lines)-1]
	if !strings.Contains(failLine, "  ") {
		t.Errorf("fail line %q missing duration suffix", failLine)
	}
}

func TestStepper_StaticMode_NoActiveStep_Success(t *testing.T) {
	var buf bytes.Buffer
	s := newStaticStepper(&buf)

	// Call Success without a prior Step — should still include a duration.
	s.Success("Done")

	out := buf.String()
	if !strings.Contains(out, "Done") {
		t.Fatalf("output %q missing message", out)
	}
	if !strings.Contains(out, "  ") {
		t.Errorf("success line %q missing duration suffix", out)
	}
}

func TestStepper_StaticMode_ClearLeavesNoTrace(t *testing.T) {
	var buf bytes.Buffer
	s := &Stepper{
		w:      &buf,
		silent: true,
	}

	s.Step("Working")
	s.Clear()

	// Silent mode: no permanent trace.
	if buf.Len() != 0 {
		t.Errorf("expected no output in silent static mode, got %q", buf.String())
	}
}
