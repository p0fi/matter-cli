// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

// ReadRequest is the TLV structure for a Read Request message (opcode 0x02).
// It specifies which attributes and/or events to read from the peer.
type ReadRequest struct {
	AttributeRequests []AttributePath `tlv:"0,listarray"`
	EventRequests     []EventPath     `tlv:"1,listarray"`
	FabricFiltered    bool            `tlv:"3,bool"`
}

// ReportData is the TLV structure for a Report Data message (opcode 0x05).
// It carries attribute reports in response to a Read or Subscribe request.
type ReportData struct {
	SubscriptionID      *uint32           `tlv:"0,uint"`
	AttributeReports    []AttributeReport `tlv:"1,array"`
	MoreChunkedMessages *bool             `tlv:"3,bool"`
	SuppressResponse    *bool             `tlv:"4,bool"`
}

// AttributeReport contains either an error status or attribute data for a
// single attribute path in a ReportData message.
type AttributeReport struct {
	Status *AttributeStatus `tlv:"0,struct"`
	Data   *AttributeData   `tlv:"1,struct"`
}

// AttributeStatus pairs an attribute path with an error status, indicating
// that the read for that path failed.
type AttributeStatus struct {
	Path   AttributePath `tlv:"0,liststruct"`
	Status StatusIB      `tlv:"1,struct"`
}

// AttributeData carries the actual data for a successfully read attribute.
type AttributeData struct {
	DataVersion uint32        `tlv:"0,uint"`
	Path        AttributePath `tlv:"1,liststruct"`
	Data        []byte        `tlv:"2,rawtlv"`
}
