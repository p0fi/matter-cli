// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

import (
	"bytes"

	"github.com/p0fi/matter-cli/internal/tlv"
)

// attrGroupKey identifies the logical attribute value a report belongs to,
// independent of ListIndex.
type attrGroupKey struct {
	endpoint  uint16
	cluster   uint32
	attribute uint32
}

// reassembleListChunks merges spec-level list-chunking fragments into one
// AttributeReport per logical attribute value. Per the Matter spec
// (data_model/Encoding-Specification.adoc, "Lists"), a device that cannot fit
// a whole list attribute in one AttributeDataIB instead sends a "replace with
// this (possibly empty) array" marker report (Path.ListIndex absent) followed
// by one "append this item" report per remaining item (Path.ListIndex
// explicit null), in order.
//
// Fragments for one attribute are assumed contiguous within the full,
// already-flattened sequence of reports for one Read (i.e. never interleaved
// with another path's reports). This matches the C++ reference
// implementation: Engine::BuildSingleReportDataAttributeReportIBs
// (src/app/reporting/Engine.cpp) iterates dirty paths one at a time and, on
// running out of space mid-list, saves an AttributeEncodeState and stops
// encoding entirely for that message rather than moving to the next path; the
// following chunk resumes the iterator at that same still-incomplete path via
// GetAttributeEncodeState/SetAttributeEncodeState before considering any
// other path.
//
// An append fragment with no matching preceding marker is out-of-spec. It is
// passed through as its own unmerged AttributeReport rather than merged or
// dropped, so it flows into the caller's existing raw-TLV-decode-failure
// display instead of failing the whole Read.
func reassembleListChunks(reports []AttributeReport) []AttributeReport {
	merged := make([]AttributeReport, 0, len(reports))

	var (
		haveOpen bool
		openKey  attrGroupKey
		openBase AttributeReport
		isArray  bool
		appends  [][]byte
	)

	flush := func() {
		if !haveOpen {
			return
		}
		merged = append(merged, finalizeListGroup(openBase, appends))
		haveOpen = false
		appends = nil
	}

	for _, r := range reports {
		key, ok := attributeReportKey(r)
		if !ok {
			flush()
			merged = append(merged, r)
			continue
		}

		if r.Data.Path.ListIndex.IsNull() {
			// An append fragment continues the currently open group only if
			// that group is itself list-typed; otherwise there is no valid
			// preceding whole-list marker to append to.
			if haveOpen && isArray && key == openKey {
				appends = append(appends, r.Data.Data)
				continue
			}
			flush()
			merged = append(merged, r)
			continue
		}

		flush()
		haveOpen = true
		openKey = key
		openBase = r
		isArray = isListElement(r.Data.Data)
	}
	flush()

	return merged
}

// attributeReportKey extracts the grouping key for a report carrying
// attribute data. It returns false for status-only reports and for reports
// whose path is missing the endpoint/cluster/attribute components that
// AttributeDataIB always carries.
func attributeReportKey(r AttributeReport) (attrGroupKey, bool) {
	if r.Data == nil {
		return attrGroupKey{}, false
	}
	p := r.Data.Path
	if p.EndpointID == nil || p.ClusterID == nil || p.AttributeID == nil {
		return attrGroupKey{}, false
	}
	return attrGroupKey{endpoint: *p.EndpointID, cluster: *p.ClusterID, attribute: *p.AttributeID}, true
}

// isListElement reports whether raw is a captured TLV array element (as
// AttributeData.Data's `rawtlv` field always stores it: re-encoded with an
// anonymous tag, so the element type alone occupies the leading byte).
func isListElement(raw []byte) bool {
	return len(raw) >= 2 && tlv.ElementType(raw[0]) == tlv.TypeArray
}

// finalizeListGroup produces the AttributeReport for one logical attribute
// value, splicing any accumulated append-fragment items into base's array
// when there are any. With no accumulated appends, base is returned
// unchanged.
func finalizeListGroup(base AttributeReport, appends [][]byte) AttributeReport {
	if len(appends) == 0 {
		return base
	}
	spliced, ok := spliceListItems(base.Data.Data, appends)
	if !ok {
		return base
	}
	data := *base.Data
	data.Data = spliced
	out := base
	out.Data = &data
	return out
}

// spliceListItems rebuilds a captured TLV array element's bytes with extra
// items appended after its existing ones. base must be a captured array
// element (control byte, zero or more anonymous-tagged items, end-of-container
// byte, per isListElement/captureElement in internal/tlv); each entry in
// items must be one fully self-contained, anonymous-tagged TLV element, which
// is exactly what AttributeData.Data holds for an append fragment.
func spliceListItems(base []byte, items [][]byte) ([]byte, bool) {
	if !isListElement(base) {
		return nil, false
	}
	inner := base[1 : len(base)-1]

	var buf bytes.Buffer
	buf.WriteByte(base[0])
	buf.Write(inner)
	for _, item := range items {
		buf.Write(item)
	}
	buf.WriteByte(byte(tlv.TypeEndOfContainer))
	return buf.Bytes(), true
}
