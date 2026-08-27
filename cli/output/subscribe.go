// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// SubscribeRecord is one emitted attribute report from a foreground
// subscription stream. Value carries the natively typed decoded value for
// JSON/YAML consumers; Display carries a pre-formatted human-readable string
// for table output. When the attribute's TLV cannot be decoded, Value is nil
// and Raw/DecodeError describe the failure instead, so one unfamiliar value
// doesn't terminate an otherwise healthy stream.
type SubscribeRecord struct {
	Timestamp   time.Time `json:"timestamp" yaml:"timestamp"`
	NodeID      uint64    `json:"node_id" yaml:"node_id"`
	Endpoint    uint16    `json:"endpoint" yaml:"endpoint"`
	ClusterID   uint32    `json:"cluster_id" yaml:"cluster_id"`
	Cluster     string    `json:"cluster" yaml:"cluster"`
	AttributeID uint32    `json:"attribute_id" yaml:"attribute_id"`
	Attribute   string    `json:"attribute" yaml:"attribute"`
	Value       any       `json:"value" yaml:"value"`
	DataVersion *uint32   `json:"data_version,omitempty" yaml:"data_version,omitempty"`
	Raw         string    `json:"raw,omitempty" yaml:"raw,omitempty"`
	DecodeError string    `json:"decode_error,omitempty" yaml:"decode_error,omitempty"`

	// Display is a pre-formatted, human-readable rendering of Value, used
	// only by the table encoder. JSON/YAML consumers get Value directly so
	// they never have to parse a display string back into a native type.
	Display string `json:"-" yaml:"-"`
}

// SubscribeEncoder writes SubscribeRecords to an output stream, one per
// Encode call. A returned error is terminal: the caller must stop the
// subscription rather than continue writing to a broken stream.
type SubscribeEncoder interface {
	Encode(rec SubscribeRecord) error
}

// NewSubscribeEncoder returns a SubscribeEncoder for the given format
// ("table", "json", or "yaml", matching FormatType). An empty format
// defaults to table for a TTY stdout and NDJSON for a pipe, consistent with
// New(). Unlike New()'s JSON branch, the JSON subscribe encoder is never
// pretty-printed regardless of TTY: NDJSON requires exactly one compact
// object per line so an indefinite stream can be parsed incrementally.
func NewSubscribeEncoder(format string, w io.Writer) SubscribeEncoder {
	switch FormatType(format) {
	case FormatJSON:
		return &ndjsonSubscribeEncoder{w: w}
	case FormatYAML:
		return &yamlSubscribeEncoder{w: w}
	case FormatTable:
		return &tableSubscribeEncoder{w: w}
	default:
		if IsTTY() {
			return &tableSubscribeEncoder{w: w}
		}
		return &ndjsonSubscribeEncoder{w: w}
	}
}

// ndjsonSubscribeEncoder writes one compact JSON object per line.
type ndjsonSubscribeEncoder struct {
	w   io.Writer
	enc *json.Encoder
}

// Encode writes rec as a single-line JSON object.
func (e *ndjsonSubscribeEncoder) Encode(rec SubscribeRecord) error {
	if e.enc == nil {
		e.enc = json.NewEncoder(e.w)
	}
	if err := e.enc.Encode(rec); err != nil {
		return fmt.Errorf("encoding ndjson record: %w", err)
	}
	return nil
}

// yamlSubscribeEncoder writes each record as a separate YAML document
// separated by "---", forming a valid multi-document YAML stream.
type yamlSubscribeEncoder struct {
	w io.Writer
}

// Encode writes rec as a "---"-delimited YAML document.
func (e *yamlSubscribeEncoder) Encode(rec SubscribeRecord) error {
	if _, err := fmt.Fprintln(e.w, "---"); err != nil {
		return fmt.Errorf("writing yaml document separator: %w", err)
	}
	b, err := yaml.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encoding yaml record: %w", err)
	}
	if _, err := e.w.Write(b); err != nil {
		return fmt.Errorf("writing yaml record: %w", err)
	}
	return nil
}

// tableSubscribeEncoder writes a header row once, followed by one
// timestamped row per record.
type tableSubscribeEncoder struct {
	w    io.Writer
	once sync.Once
}

// Encode writes rec as a timestamped table row, printing the header first if
// this is the first call.
func (e *tableSubscribeEncoder) Encode(rec SubscribeRecord) error {
	var headerErr error
	e.once.Do(func() {
		_, headerErr = fmt.Fprintln(e.w, Header(fmt.Sprintf("%-12s  %-6s  %-4s  %-16s  %-20s  %-10s  %s",
			"TIME", "NODE", "EP", "CLUSTER", "ATTRIBUTE", "VERSION", "VALUE")))
	})
	if headerErr != nil {
		return fmt.Errorf("writing table header: %w", headerErr)
	}

	version := "-"
	if rec.DataVersion != nil {
		version = strconv.FormatUint(uint64(*rec.DataVersion), 10)
	}
	value := rec.Display
	if rec.DecodeError != "" {
		value = fmt.Sprintf("%s %s", value, Muted(fmt.Sprintf("(decode error: %s)", rec.DecodeError)))
	}

	_, err := fmt.Fprintf(e.w, "%-12s  %-6d  %-4d  %-16s  %-20s  %-10s  %s\n",
		rec.Timestamp.Format("15:04:05.000"),
		rec.NodeID, rec.Endpoint, rec.Cluster, rec.Attribute, version, value)
	if err != nil {
		return fmt.Errorf("writing table row: %w", err)
	}
	return nil
}
