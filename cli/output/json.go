// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"encoding/json"
	"fmt"
	"io"
)

// JSONFormatter formats data as JSON. When Pretty is true, the output is
// indented for human readability; otherwise it is compact for piping.
type JSONFormatter struct {
	Pretty bool
}

// Format writes data as JSON to w.
func (f *JSONFormatter) Format(w io.Writer, data any) error {
	enc := json.NewEncoder(w)
	if f.Pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("encoding json: %w", err)
	}
	return nil
}
