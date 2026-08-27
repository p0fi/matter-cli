// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func sampleRecords() []SubscribeRecord {
	dv := uint32(7)
	return []SubscribeRecord{
		{
			Timestamp:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			NodeID:      1,
			Endpoint:    1,
			ClusterID:   0x0006,
			Cluster:     "OnOff",
			AttributeID: 0x0000,
			Attribute:   "OnOff",
			Value:       true,
			DataVersion: &dv,
			Display:     "true",
		},
		{
			Timestamp:   time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC),
			NodeID:      1,
			Endpoint:    1,
			ClusterID:   0x0006,
			Cluster:     "OnOff",
			AttributeID: 0x0000,
			Attribute:   "OnOff",
			Value:       nil,
			Raw:         "0xdeadbeef",
			DecodeError: "unexpected element type",
			Display:     "0xdeadbeef",
		},
	}
}

func TestNDJSONSubscribeEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewSubscribeEncoder("json", &buf)

	for _, rec := range sampleRecords() {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (one JSON object per line): %q", len(lines), buf.String())
	}
	for i, line := range lines {
		if strings.Contains(line, "\n") {
			t.Errorf("line %d spans multiple lines (not compact NDJSON): %q", i, line)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
	}

	// The first record's boolean value must round-trip as a native bool, not
	// a display string.
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if v, ok := first["value"].(bool); !ok || v != true {
		t.Errorf("value = %#v, want native bool true", first["value"])
	}

	// The second record must carry the raw-fallback shape.
	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if second["value"] != nil {
		t.Errorf("value = %#v, want nil for a decode failure", second["value"])
	}
	if second["raw"] != "0xdeadbeef" {
		t.Errorf("raw = %#v, want 0xdeadbeef", second["raw"])
	}
	if second["decode_error"] == nil {
		t.Error("decode_error should be present for a decode failure")
	}
}

// TestNDJSONSubscribeEncoder_NeverPretty verifies that the JSON subscribe
// encoder never indents, even when New()'s general JSON formatter would
// (IsTTY() == true), since NDJSON requires exactly one object per line.
func TestNDJSONSubscribeEncoder_NeverPretty(t *testing.T) {
	var buf bytes.Buffer
	enc := NewSubscribeEncoder("json", &buf)
	if err := enc.Encode(sampleRecords()[0]); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "\n") != 1 {
		t.Errorf("output = %q, want exactly one line", buf.String())
	}
}

func TestYAMLSubscribeEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewSubscribeEncoder("yaml", &buf)

	for _, rec := range sampleRecords() {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}

	docs := strings.Split(buf.String(), "---\n")
	// The stream starts with "---\n" so the first split element is empty.
	var nonEmpty []string
	for _, d := range docs {
		if strings.TrimSpace(d) != "" {
			nonEmpty = append(nonEmpty, d)
		}
	}
	if len(nonEmpty) != 2 {
		t.Fatalf("got %d non-empty YAML documents, want 2: %q", len(nonEmpty), buf.String())
	}

	for i, d := range nonEmpty {
		var m map[string]any
		if err := yaml.Unmarshal([]byte(d), &m); err != nil {
			t.Errorf("document %d is not valid YAML: %v", i, err)
		}
	}

	var first map[string]any
	if err := yaml.Unmarshal([]byte(nonEmpty[0]), &first); err != nil {
		t.Fatal(err)
	}
	if v, ok := first["value"].(bool); !ok || v != true {
		t.Errorf("value = %#v, want native bool true", first["value"])
	}
}

func TestTableSubscribeEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewSubscribeEncoder("table", &buf)

	for _, rec := range sampleRecords() {
		if err := enc.Encode(rec); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 { // header + 2 rows
		t.Fatalf("got %d lines, want 3 (header + 2 rows): %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "TIME") || !strings.Contains(lines[0], "VALUE") {
		t.Errorf("header row = %q, missing expected column labels", lines[0])
	}
	if !strings.Contains(lines[1], "true") {
		t.Errorf("row 1 = %q, want it to contain the decoded value", lines[1])
	}
	if !strings.Contains(lines[2], "decode error") {
		t.Errorf("row 2 = %q, want a decode-error annotation", lines[2])
	}

	// The header must only be printed once across multiple Encode calls.
	headerCount := strings.Count(buf.String(), "TIME")
	if headerCount != 1 {
		t.Errorf("header printed %d times, want 1", headerCount)
	}
}

func TestSubscribeEncoderWriteFailureIsTerminal(t *testing.T) {
	enc := NewSubscribeEncoder("json", &failingWriter{})
	if err := enc.Encode(sampleRecords()[0]); err == nil {
		t.Error("expected an error when the underlying writer fails")
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("simulated write failure")
}
