// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
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
type Stepper struct {
	w       io.Writer
	animate bool // true = use spinner animation
	verbose bool // true = delegate to slog

	mu     sync.Mutex
	msg    string // current step message
	active bool
	stopCh chan struct{}
	doneCh chan struct{}
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

// Step marks the previous step as complete (green) and starts a new step.
func (s *Stepper) Step(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Complete previous step.
	if s.active {
		s.completeCurrentLocked(true)
	}

	s.msg = msg
	s.active = true

	if s.animate {
		s.stopCh = make(chan struct{})
		s.doneCh = make(chan struct{})
		go s.spin()
	} else {
		// Static mode: print with dim dot (in-progress).
		fmt.Fprintf(s.w, "%s %s\n", Dim("●"), msg)
	}
}

// Success marks the current step as complete and prints a success message.
func (s *Stepper) Success(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		s.completeCurrentLocked(true)
	}

	fmt.Fprintf(s.w, "%s %s\n", Success("✓"), msg)
}

// Fail marks the current step as failed and prints an error message.
func (s *Stepper) Fail(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active {
		s.completeCurrentLocked(false)
	}

	fmt.Fprintf(s.w, "%s %s\n", Error("✗"), msg)
}

// completeCurrentLocked finishes the current step. Must be called with mu held.
func (s *Stepper) completeCurrentLocked(ok bool) {
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

	if s.animate {
		// Overwrite the spinner line.
		fmt.Fprintf(s.w, "\r\033[K%s %s\n", icon, s.msg)
	}
	// In verbose and static mode, the line was already printed; we don't overwrite.

	s.active = false
	s.msg = ""
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
