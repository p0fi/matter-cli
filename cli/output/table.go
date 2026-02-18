// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"strings"
)

// TableData represents data that can be rendered as a table.
type TableData struct {
	Headers []string
	Rows    [][]string
}

// TableFormatter formats TableData as a styled text table using lipgloss.
type TableFormatter struct{}

// Format writes data as a styled table to w. If data is not a *TableData, it
// falls back to fmt.Fprintf.
func (f *TableFormatter) Format(w io.Writer, data any) error {
	td, ok := data.(*TableData)
	if !ok {
		_, err := fmt.Fprintf(w, "%v\n", data)
		return err
	}
	if len(td.Headers) == 0 {
		return nil
	}

	// Determine column widths.
	widths := make([]int, len(td.Headers))
	for i, h := range td.Headers {
		widths[i] = len(h)
	}
	for _, row := range td.Rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	sep := "  "

	// Print header.
	var hdr strings.Builder
	for i, h := range td.Headers {
		if i > 0 {
			hdr.WriteString(sep)
		}
		hdr.WriteString(pad(h, widths[i]))
	}
	fmt.Fprintln(w, Header(hdr.String()))

	// Print separator line.
	var divider strings.Builder
	for i, width := range widths {
		if i > 0 {
			divider.WriteString(sep)
		}
		divider.WriteString(strings.Repeat("─", width))
	}
	fmt.Fprintln(w, Dim(divider.String()))

	// Print rows with alternating subtle styling.
	for _, row := range td.Rows {
		var line strings.Builder
		for i := range td.Headers {
			if i > 0 {
				line.WriteString(sep)
			}
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			line.WriteString(pad(cell, widths[i]))
		}
		fmt.Fprintln(w, line.String())
	}
	return nil
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
