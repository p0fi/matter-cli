// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// LogHandler is a custom slog.Handler that produces compact, colored log
// output with timestamps, aligned level names, and level-based coloring.
//
//	23:41:21 DEBUG  controller: sending          bytes=42 to=192.168.1.100:5540
//	23:41:21 WARN   controller: receive error    err=timeout
type LogHandler struct {
	w     io.Writer
	mu    sync.Mutex
	level slog.Leveler
	attrs []slog.Attr
}

// NewLogHandler returns a handler that writes colored, compact log lines to w.
func NewLogHandler(w io.Writer, level slog.Leveler) *LogHandler {
	return &LogHandler{w: w, level: level}
}

func (h *LogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	// Timestamp at second resolution.
	ts := Dim(r.Time.Format("15:04:05"))

	// Level name, padded to 5 chars, colored by level.
	var level string
	switch {
	case r.Level >= slog.LevelError:
		level = Error(fmt.Sprintf("%-5s", "ERROR"))
	case r.Level >= slog.LevelWarn:
		level = Warning(fmt.Sprintf("%-5s", "WARN"))
	case r.Level >= slog.LevelInfo:
		level = Info(fmt.Sprintf("%-5s", "INFO"))
	default:
		level = Dim(fmt.Sprintf("%-5s", "DEBUG"))
	}

	msg := r.Message

	// Collect attributes.
	var attrStr string
	writeAttr := func(a slog.Attr) {
		if a.Key == "" {
			return
		}
		attrStr += "  " + Dim(a.Key+"=") + Dim(a.Value.String())
	}
	for _, a := range h.attrs {
		writeAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(a)
		return true
	})

	line := fmt.Sprintf("%s %s  %s%s\n", ts, level, msg, attrStr)

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, line)
	return err
}

func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandler{
		w:     h.w,
		level: h.level,
		attrs: append(h.attrs, attrs...),
	}
}

func (h *LogHandler) WithGroup(_ string) slog.Handler {
	// Group support is not needed for this CLI tool.
	return h
}
