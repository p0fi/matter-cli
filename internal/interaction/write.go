// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package interaction

// WriteRequest is the TLV structure for a Write Request message (opcode 0x06).
// It carries one or more attribute writes to be applied on the peer.
// SuppressResponse and TimedRequest are non-pointer bools so they are always
// encoded on the wire (as false) — the CHIP SDK WriteHandler requires both
// fields to be present and returns INVALID_ACTION if either is absent.
// WriteRequests uses "listarray" (TLV ARRAY outer + LIST inner) because CHIP
// SDK's AttributeDataIBs::Builder/Parser uses kTLVType_Array for the outer
// container while each AttributeDataIB element is a kTLVType_List.
type WriteRequest struct {
	SuppressResponse    bool             `tlv:"0,bool"`
	TimedRequest        bool             `tlv:"1,bool"`
	WriteRequests       []AttributeWrite `tlv:"2,listarray"`
	MoreChunkedMessages *bool            `tlv:"3,bool"`
}

// AttributeWrite specifies a single attribute value to write, including the
// target path and the raw TLV-encoded data.
// Data must contain a single anonymous-tagged TLV element (as produced by
// tlv.Writer.PutUnsignedInt/PutBool/etc. with AnonymousTag()). The marshaller
// re-encodes it at context tag 2 inline — NOT wrapped in an octet string —
// which is what the Matter IM spec requires for AttributeDataIB.Data.
type AttributeWrite struct {
	DataVersion *uint32       `tlv:"0,uint"`
	Path        AttributePath `tlv:"1,liststruct"`
	Data        []byte        `tlv:"2,rawtlv"`
}

// WriteResponse is the TLV structure for a Write Response message (opcode 0x07).
// It contains per-attribute write status results.
type WriteResponse struct {
	WriteResponses []AttributeStatus `tlv:"0,array"`
}
