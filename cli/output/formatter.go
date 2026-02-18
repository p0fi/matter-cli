// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package output provides output formatting for matter-cli commands.
// It supports table, JSON, and YAML formats with automatic TTY detection.
package output

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// Formatter formats data for output.
type Formatter interface {
	// Format writes the formatted representation of data to w.
	Format(w io.Writer, data any) error
}

// FormatType identifies an output format.
type FormatType string

const (
	// FormatTable renders data as an aligned text table.
	FormatTable FormatType = "table"
	// FormatJSON renders data as JSON.
	FormatJSON FormatType = "json"
	// FormatYAML renders data as YAML.
	FormatYAML FormatType = "yaml"
)

// IsTTY reports whether stdout is a terminal.
func IsTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// New returns a Formatter for the given format type. If format is empty, it
// selects table for TTY and JSON for pipes.
func New(format string) Formatter {
	switch FormatType(format) {
	case FormatJSON:
		return &JSONFormatter{Pretty: IsTTY()}
	case FormatYAML:
		return &JSONFormatter{Pretty: true} // YAML stub — uses pretty JSON for now
	case FormatTable:
		return &TableFormatter{}
	default:
		if IsTTY() {
			return &TableFormatter{}
		}
		return &JSONFormatter{Pretty: false}
	}
}
