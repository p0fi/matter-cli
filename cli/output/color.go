// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Color helpers respect the NO_COLOR environment variable
// (https://no-color.org). When NO_COLOR is set, all style functions
// return the input string unchanged.
//
// Colors are defined using ANSI color numbers (0–15) rather than hex values so
// that they automatically adapt to whatever palette the user has configured in
// their terminal emulator (e.g. Solarized, Nord, Dracula, Catppuccin, …).
//
// Standard ANSI palette:
//
//	0  Black         8  Bright Black (Dark Gray)
//	1  Red           9  Bright Red
//	2  Green        10  Bright Green
//	3  Yellow       11  Bright Yellow
//	4  Blue         12  Bright Blue
//	5  Magenta      13  Bright Magenta
//	6  Cyan         14  Bright Cyan
//	7  Light Gray   15  Bright White

var (
	// Core colors — ANSI numbers, resolved by the terminal's own palette.
	colorGreen     = lipgloss.ANSIColor(10) // Bright Green  → success
	colorRed       = lipgloss.ANSIColor(9)  // Bright Red    → errors
	colorYellow    = lipgloss.ANSIColor(3)  // Yellow → warnings
	colorBlue      = lipgloss.ANSIColor(12) // Bright Blue   → info / headers
	colorCyan      = lipgloss.ANSIColor(14) // Bright Cyan   → labels / commands
	colorMagenta   = lipgloss.ANSIColor(13) // Bright Magenta→ accents / IDs
	colorLightGray = lipgloss.ANSIColor(7)  // Light Gray     → values
	colorGray      = lipgloss.ANSIColor(8)  // Bright Black (Dark Gray) → muted/secondary
	colorBlack     = lipgloss.ANSIColor(0)  // Black         → text on filled badges

	// StyleSuccess renders text in green.
	StyleSuccess = lipgloss.NewStyle().Foreground(colorGreen)
	// StyleError renders text in red and bold.
	StyleError = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	// StyleWarning renders text in yellow.
	StyleWarning = lipgloss.NewStyle().Foreground(colorYellow)
	// StyleInfo renders text in blue.
	StyleInfo = lipgloss.NewStyle().Foreground(colorBlue)
	// StyleBold renders text in bold.
	StyleBold = lipgloss.NewStyle().Bold(true)
	// StyleDim renders text with reduced intensity using the terminal's own
	// faint/dim attribute (ANSI SGR 2) rather than a fixed gray color.
	StyleDim = lipgloss.NewStyle().Faint(true)

	// StyleLabel renders a key/label (e.g. "Vendor:") in cyan bold.
	StyleLabel = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	// StyleValue renders a value in light gray (ANSI 7).
	StyleValue = lipgloss.NewStyle().Foreground(colorLightGray)
	// StyleAccent renders text in magenta (used for IDs, hex values).
	StyleAccent = lipgloss.NewStyle().Foreground(colorMagenta)
	// StyleHeader renders table/section headers in blue bold.
	StyleHeader = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	// StyleMuted renders secondary/less-important text in ANSI color 8
	// (Bright Black / Dark Gray). This is the canonical "comment" color across
	// terminal themes (Solarized, Nord, Dracula, Catppuccin, …) and gives a
	// reliably subdued appearance in both dark and light palettes, unlike the
	// faint attribute (SGR 2) which many terminals render inconsistently.
	StyleMuted = lipgloss.NewStyle().Foreground(colorGray)

	// StyleTipBadge renders the "TIP" badge that prefixes advisory hints.
	// It is a filled badge rather than a glyph on purpose: the stepper owns
	// the leading-icon vocabulary (● ✓ ✗ ▲), so a hint that borrowed one of
	// those would read as another step in the sequence.
	StyleTipBadge = lipgloss.NewStyle().Foreground(colorBlack).Background(colorBlue).Bold(true)

	// StyleCommand renders command names in cyan.
	StyleCommand = lipgloss.NewStyle().Foreground(colorCyan)
	// StyleFlag renders flag names in yellow.
	StyleFlag = lipgloss.NewStyle().Foreground(colorYellow)
)

// NoColor reports whether the NO_COLOR environment variable is set.
func NoColor() bool {
	_, ok := os.LookupEnv("NO_COLOR")
	return ok
}

func render(style lipgloss.Style, s string) string {
	if NoColor() {
		return s
	}
	return style.Render(s)
}

// Success formats text as a success message (green).
func Success(s string) string { return render(StyleSuccess, s) }

// Error formats text as an error message (red bold).
func Error(s string) string { return render(StyleError, s) }

// Warning formats text as a warning message (yellow).
func Warning(s string) string { return render(StyleWarning, s) }

// Info formats text as an informational message (blue).
func Info(s string) string { return render(StyleInfo, s) }

// Bold formats text in bold.
func Bold(s string) string { return render(StyleBold, s) }

// Dim formats text with reduced intensity.
func Dim(s string) string { return render(StyleDim, s) }

// Label formats a key/label in cyan bold.
func Label(s string) string { return render(StyleLabel, s) }

// Value formats a value in the default foreground color.
func Value(s string) string { return render(StyleValue, s) }

// Accent formats text in magenta (for IDs, hex values).
func Accent(s string) string { return render(StyleAccent, s) }

// Header formats section headers in blue bold.
func Header(s string) string { return render(StyleHeader, s) }

// Muted formats secondary/less important text.
func Muted(s string) string { return render(StyleMuted, s) }

// Command formats a command name in cyan.
func Command(s string) string { return render(StyleCommand, s) }

// Flag formats a flag name in yellow.
func Flag(s string) string { return render(StyleFlag, s) }

// TipBadge returns the "TIP" badge used to mark advisory hints.
//
// Under NO_COLOR it degrades to "TIP:" rather than the badge's padded text:
// the padding only exists to give the filled background room to breathe, and
// without a background it just reads as ragged indentation.
func TipBadge() string {
	if NoColor() {
		return "TIP:"
	}
	return render(StyleTipBadge, " TIP ")
}

// Tip formats an advisory hint: an indented TIP badge followed by body.
//
// Hints are deliberately unlike every other line a command prints. They carry
// no stepper icon, they are indented, and the badge is filled, so a suggestion
// the user is free to ignore cannot be mistaken for a step that just ran.
//
// body is emitted verbatim so callers can highlight a command or path within
// it; wrap plain prose in Muted to get the standard subdued hint text.
func Tip(body string) string {
	return fmt.Sprintf("  %s %s", TipBadge(), body)
}

// visWidth returns the visible display width of s, ignoring ANSI escape codes.
func visWidth(s string) int { return lipgloss.Width(s) }

// SuccessIcon returns a styled checkmark.
func SuccessIcon() string { return Success("✓") }

// ErrorIcon returns a styled X mark.
func ErrorIcon() string { return Error("✗") }

// InfoIcon returns a styled info marker.
func InfoIcon() string { return Info("●") }

// WarningIcon returns a styled warning marker.
func WarningIcon() string { return Warning("▲") }

// SpinnerIcon returns a styled progress/spinner marker.
func SpinnerIcon() string { return Info("◉") }
