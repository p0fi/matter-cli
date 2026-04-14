// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Spinner characters (braille pattern).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Stepper manages step-by-step progress output with optional spinner animation.
//
// Three modes based on context:
//   - Verbose: delegates all output to slog so steps and debug logs share one format.
//   - Interactive (TTY, non-verbose): animated spinner for in-progress, green dot on completion.
//   - Piped (non-TTY, non-verbose): static lines with dots to stdout.
//
// When silent is true (NewProgressStepper), completed steps leave no permanent
// trace: in animate mode the spinner line is overwritten in-place; in static
// mode nothing is printed at all.
//
// Every completed step and terminal call (Success/Fail) appends its elapsed
// duration as muted text, e.g. "● Establishing PASE session  1.2s".
type Stepper struct {
	w       io.Writer
	animate bool // true = use spinner animation
	verbose bool // true = delegate to slog
	silent  bool // true = no permanent trace per completed step

	mu               sync.Mutex
	msg              string    // current step message
	startedAt        time.Time // when the current step was started
	processStartedAt time.Time // when the first Step was called; never reset
	firstStep        bool      // true until the first step has been completed
	active           bool
	stopCh           chan struct{}
	doneCh           chan struct{}
}

// formatDuration formats a duration for display: milliseconds below 1 s,
// one-decimal seconds below 60 s, and "XmYs" above that.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
}

// NewStepper creates a Stepper that writes to w. When verbose is true, all
// output is routed through slog for a unified log format. Otherwise, the
// stepper uses a spinner animation on TTY or static dots when piped.
func NewStepper(w io.Writer, verbose bool) *Stepper {
	return &Stepper{
		w:       w,
		verbose: verbose,
		animate: !verbose && IsTTY() && !NoColor(),
	}
}

// NewProgressStepper is like NewStepper but uses silent mode: completed steps
// leave no permanent trace. In animate (TTY) mode the spinner overwrites a
// single line continuously. In static (pipe) mode nothing is printed at all.
func NewProgressStepper(w io.Writer, verbose bool) *Stepper {
	return &Stepper{
		w:       w,
		verbose: verbose,
		animate: !verbose && IsTTY() && !NoColor(),
		silent:  true,
	}
}

// Step marks the previous step as complete (green) and starts a new step.
func (s *Stepper) Step(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Complete previous step.
	if s.active {
		s.completeCurrentLocked(true)
	}

	s.msg = msg
	s.startedAt = time.Now()
	if s.processStartedAt.IsZero() {
		s.processStartedAt = s.startedAt
		s.firstStep = true
	}
	s.active = true

	if s.verbose {
		slog.Info(msg)
	}

	if s.animate {
		s.stopCh = make(chan struct{})
		s.doneCh = make(chan struct{})
		go s.spin()
	}
	// Static (piped) mode: defer printing until the step completes so we can
	// include the elapsed duration on the same line.
}

// Success marks the current step as complete and prints a success message with
// the total elapsed time since the first Step() call (or since this call if no
// prior step was ever started).
func (s *Stepper) Success(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := s.processStartedAt
	if start.IsZero() {
		start = time.Now()
	}

	if s.active {
		s.completeCurrentLocked(true)
	}

	elapsed := time.Since(start)
	dur := Dim(formatDuration(elapsed))
	if s.verbose {
		slog.Info(msg, slog.Duration("elapsed", elapsed))
	} else {
		fmt.Fprintf(s.w, "%s %s  %s\n", Success("✓"), msg, dur)
	}
}

// Fail marks the current step as failed and prints an error message with
// the total elapsed time since the first Step() call (or since this call if no
// prior step was ever started).
func (s *Stepper) Fail(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start := s.processStartedAt
	if start.IsZero() {
		start = time.Now()
	}

	if s.active {
		s.completeCurrentLocked(false)
	}

	elapsed := time.Since(start)
	dur := Dim(formatDuration(elapsed))
	if s.verbose {
		slog.Error(msg, slog.Duration("elapsed", elapsed))
	} else {
		fmt.Fprintf(s.w, "%s %s  %s\n", Error("✗"), msg, dur)
	}
}

// completeCurrentLocked finishes the current step. Must be called with mu held.
func (s *Stepper) completeCurrentLocked(ok bool) {
	elapsed := time.Since(s.startedAt)

	if s.animate && s.stopCh != nil {
		close(s.stopCh)
		// Release lock while waiting for spinner goroutine to finish,
		// since spin() also writes to w.
		s.mu.Unlock()
		<-s.doneCh
		s.mu.Lock()
		s.stopCh = nil
		s.doneCh = nil
	}

	icon := Success("●")
	if !ok {
		icon = Error("●")
	}

	// The first step is an announcement ("Commissioning device … as node N"); it
	// completes almost immediately so its duration is meaningless noise.
	isFirst := s.firstStep
	s.firstStep = false

	if s.verbose {
		if ok {
			slog.Info(s.msg, slog.Duration("elapsed", elapsed))
		} else {
			slog.Error(s.msg, slog.Duration("elapsed", elapsed))
		}
	} else if !s.silent {
		line := fmt.Sprintf("%s %s", icon, s.msg)
		if !isFirst {
			line += "  " + Dim(formatDuration(elapsed))
		}
		if s.animate {
			fmt.Fprintf(s.w, "\r\033[K%s\n", line)
		} else {
			fmt.Fprintf(s.w, "%s\n", line)
		}
	}
	// In silent mode, completed steps leave no permanent trace.

	s.active = false
	s.msg = ""
}

// Clear stops the current step (marking it successful) without printing an
// additional status line. Use this before rendering output so the spinner is
// cleanly stopped and the terminal cursor is left on a fresh line.
func (s *Stepper) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		s.completeCurrentLocked(true)
	}
	// In silent+animate mode, erase the last spinner frame so the cursor is on
	// a clean line before the caller renders its own output.
	if s.animate && s.silent {
		fmt.Fprintf(s.w, "\r\033[K")
	}
}

// spin runs the spinner animation until stopCh is closed.
func (s *Stepper) spin() {
	defer close(s.doneCh)

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	// Print initial frame.
	fmt.Fprintf(s.w, "\r%s %s", Dim(spinnerFrames[i]), s.msg)
	i++

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			frame := spinnerFrames[i%len(spinnerFrames)]
			fmt.Fprintf(s.w, "\r%s %s", Dim(frame), s.msg)
			i++
		}
	}
}
