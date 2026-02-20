// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Color helpers respect the NO_COLOR environment variable
// (https://no-color.org). When NO_COLOR is set, all style functions
// return the input string unchanged.

var (
	// Core colors.
	colorGreen   = lipgloss.Color("#04B575")
	colorRed     = lipgloss.Color("#FF4672")
	colorYellow  = lipgloss.Color("#FFBD2E")
	colorBlue    = lipgloss.Color("#51AFEF")
	colorCyan    = lipgloss.Color("#56B6C2")
	colorMagenta = lipgloss.Color("#C678DD")
	colorDim     = lipgloss.Color("#5C6370")
	colorWhite   = lipgloss.Color("#E5E5E5")

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
	// StyleDim renders text with reduced intensity.
	StyleDim = lipgloss.NewStyle().Foreground(colorDim)

	// StyleLabel renders a key/label (e.g. "Vendor:") in cyan bold.
	StyleLabel = lipgloss.NewStyle().Foreground(colorCyan).Bold(true)
	// StyleValue renders a value in white.
	StyleValue = lipgloss.NewStyle().Foreground(colorWhite)
	// StyleAccent renders text in magenta (used for IDs, hex values).
	StyleAccent = lipgloss.NewStyle().Foreground(colorMagenta)
	// StyleHeader renders table/section headers in blue bold.
	StyleHeader = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	// StyleMuted renders secondary text in dim gray.
	StyleMuted = StyleDim

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

// Value formats a value in white.
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
