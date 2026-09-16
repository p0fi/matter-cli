// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"bytes"
	"testing"

	"github.com/p0fi/matter-cli/internal/tlv"
)

// rawUint encodes a bare TLV unsigned integer element with an anonymous tag,
// matching the shape AttributeData.Data (`rawtlv`) captures on the wire for a
// scalar array item.
func rawUint(v uint64) []byte {
	w := tlv.NewWriter()
	if err := w.PutUnsignedInt(tlv.AnonymousTag(), v); err != nil {
		panic(err)
	}
	return w.Bytes()
}

// rawArray encodes a TLV array element (anonymous tag) containing items,
// matching what AttributeData.Data captures for a non-chunked or "replace
// with this array" list report.
func rawArray(items ...[]byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(byte(tlv.TypeArray))
	for _, item := range items {
		buf.Write(item)
	}
	buf.WriteByte(byte(tlv.TypeEndOfContainer))
	return buf.Bytes()
}

func dataReport(endpoint uint16, cluster, attribute uint32, listIndex tlv.Optional[uint16], data []byte) AttributeReport {
	ep, cl, at := endpoint, cluster, attribute
	return AttributeReport{
		Data: &AttributeData{
			DataVersion: 1,
			Path: AttributePath{
				EndpointID:  &ep,
				ClusterID:   &cl,
				AttributeID: &at,
				ListIndex:   listIndex,
			},
			Data: data,
		},
	}
}

func markerReport(endpoint uint16, cluster, attribute uint32, data []byte) AttributeReport {
	return dataReport(endpoint, cluster, attribute, tlv.Optional[uint16]{}, data)
}

func appendReport(endpoint uint16, cluster, attribute uint32, item []byte) AttributeReport {
	return dataReport(endpoint, cluster, attribute, tlv.OptionalNull[uint16](), item)
}

func statusReport(endpoint uint16, cluster, attribute uint32, status StatusCode) AttributeReport {
	ep, cl, at := endpoint, cluster, attribute
	return AttributeReport{
		Status: &AttributeStatus{
			Path:   AttributePath{EndpointID: &ep, ClusterID: &cl, AttributeID: &at},
			Status: StatusIB{Status: uint8(status)},
		},
	}
}

// decodeArrayItems decodes a captured TLV array element's raw items as
// uint64s, for asserting merged content in tests.
func decodeArrayItems(t *testing.T, raw []byte) []uint64 {
	t.Helper()
	r := tlv.NewReader(bytes.NewReader(raw))
	if err := r.Next(); err != nil {
		t.Fatalf("reading array element: %v", err)
	}
	if r.Type() != tlv.TypeArray {
		t.Fatalf("element type = %s, want Array", r.Type())
	}
	var items []uint64
	for {
		if err := r.Next(); err != nil {
			t.Fatalf("reading array item: %v", err)
		}
		if r.Type() == tlv.TypeEndOfContainer {
			return items
		}
		v, ok := r.Value().(uint64)
		if !ok {
			t.Fatalf("item value = %v (%T), want uint64", r.Value(), r.Value())
		}
		items = append(items, v)
	}
}

func TestReassembleListChunks(t *testing.T) {
	t.Run("non-chunked reports pass through unchanged", func(t *testing.T) {
		reports := []AttributeReport{
			markerReport(1, 0x0006, 0x0000, []byte{0x09}), // plain bool true
			statusReport(1, 0x0008, 0x0000, StatusUnsupportedAttribute),
		}
		got := reassembleListChunks(reports)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if !bytes.Equal(got[0].Data.Data, reports[0].Data.Data) {
			t.Errorf("report 0 Data mutated: got %x, want %x", got[0].Data.Data, reports[0].Data.Data)
		}
		if got[1].Status == nil || got[1].Status.Status.Status != uint8(StatusUnsupportedAttribute) {
			t.Errorf("report 1 status not preserved: %+v", got[1])
		}
	})

	t.Run("chunked list starting empty is reassembled into one report", func(t *testing.T) {
		reports := []AttributeReport{
			markerReport(1, 0x001F, 0x0000, rawArray()), // empty-array marker
			appendReport(1, 0x001F, 0x0000, rawUint(10)),
			appendReport(1, 0x001F, 0x0000, rawUint(20)),
			appendReport(1, 0x001F, 0x0000, rawUint(30)),
		}
		got := reassembleListChunks(reports)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		items := decodeArrayItems(t, got[0].Data.Data)
		want := []uint64{10, 20, 30}
		if len(items) != len(want) {
			t.Fatalf("items = %v, want %v", items, want)
		}
		for i := range want {
			if items[i] != want[i] {
				t.Errorf("items[%d] = %d, want %d", i, items[i], want[i])
			}
		}
	})

	t.Run("chunked list starting with some items already present", func(t *testing.T) {
		reports := []AttributeReport{
			markerReport(1, 0x001F, 0x0000, rawArray(rawUint(1), rawUint(2))),
			appendReport(1, 0x001F, 0x0000, rawUint(3)),
		}
		got := reassembleListChunks(reports)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		items := decodeArrayItems(t, got[0].Data.Data)
		want := []uint64{1, 2, 3}
		if len(items) != len(want) {
			t.Fatalf("items = %v, want %v", items, want)
		}
		for i := range want {
			if items[i] != want[i] {
				t.Errorf("items[%d] = %d, want %d", i, items[i], want[i])
			}
		}
	})

	t.Run("chunked sequence spanning simulated message boundaries merges as one run", func(t *testing.T) {
		// Client.Read flattens all chunked ReportData messages into one
		// ordered slice before reassembly runs, so a chunk boundary is
		// invisible here -- this just confirms more than two fragments
		// (i.e. what would span 2+ messages in practice) still merge.
		reports := []AttributeReport{
			markerReport(2, 0x0025, 0x0001, rawArray()),
			appendReport(2, 0x0025, 0x0001, rawUint(1)),
			appendReport(2, 0x0025, 0x0001, rawUint(2)),
			appendReport(2, 0x0025, 0x0001, rawUint(3)),
			appendReport(2, 0x0025, 0x0001, rawUint(4)),
		}
		got := reassembleListChunks(reports)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		items := decodeArrayItems(t, got[0].Data.Data)
		if len(items) != 4 {
			t.Fatalf("items = %v, want 4 items", items)
		}
	})

	t.Run("other attributes around a chunked list are untouched and correctly separated", func(t *testing.T) {
		reports := []AttributeReport{
			markerReport(1, 0x0006, 0x0000, []byte{0x09}), // unrelated bool, before
			markerReport(1, 0x001F, 0x0000, rawArray()),
			appendReport(1, 0x001F, 0x0000, rawUint(7)),
			markerReport(1, 0x0008, 0x0000, []byte{0x04, 0x2A}), // unrelated uint8(42), after
		}
		got := reassembleListChunks(reports)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if !bytes.Equal(got[0].Data.Data, []byte{0x09}) {
			t.Errorf("report 0 = %x, want unchanged bool", got[0].Data.Data)
		}
		items := decodeArrayItems(t, got[1].Data.Data)
		if len(items) != 1 || items[0] != 7 {
			t.Errorf("merged list items = %v, want [7]", items)
		}
		if !bytes.Equal(got[2].Data.Data, []byte{0x04, 0x2A}) {
			t.Errorf("report 2 = %x, want unchanged uint8", got[2].Data.Data)
		}
	})

	t.Run("malformed: append fragment with no preceding whole-list marker collapses to one unmerged row", func(t *testing.T) {
		orphan := appendReport(1, 0x001F, 0x0000, rawUint(99))
		reports := []AttributeReport{orphan}
		got := reassembleListChunks(reports)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if !bytes.Equal(got[0].Data.Data, orphan.Data.Data) {
			t.Errorf("Data = %x, want unmerged raw fragment %x", got[0].Data.Data, orphan.Data.Data)
		}
		if !got[0].Data.Path.ListIndex.IsNull() {
			t.Error("orphan fragment's ListIndex should remain null (unmerged, unmodified)")
		}
	})

	t.Run("malformed: append fragment following a non-list marker for the same path is not merged", func(t *testing.T) {
		reports := []AttributeReport{
			markerReport(1, 0x0006, 0x0000, []byte{0x09}), // scalar, not a list
			appendReport(1, 0x0006, 0x0000, rawUint(1)),
		}
		got := reassembleListChunks(reports)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2 (scalar marker + orphan append, unmerged)", len(got))
		}
		if !bytes.Equal(got[0].Data.Data, []byte{0x09}) {
			t.Errorf("report 0 = %x, want unchanged scalar", got[0].Data.Data)
		}
		if !bytes.Equal(got[1].Data.Data, reports[1].Data.Data) {
			t.Errorf("report 1 = %x, want unmerged append fragment", got[1].Data.Data)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := reassembleListChunks(nil)
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})
}
