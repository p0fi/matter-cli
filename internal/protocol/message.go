// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package protocol implements the Matter message framing, session management,
// exchange tracking, and encrypted message codec.
package protocol

import (
	"encoding/binary"
	"fmt"
)

// Message flag bits (byte 0 of the message header).
const (
	// FlagSourceNodeID indicates the Source Node ID field is present (bit 2).
	FlagSourceNodeID byte = 1 << 2
	// FlagDSIZMask is the mask for the Destination ID Size bits (bits 0-1).
	FlagDSIZMask byte = 0x03
	// FlagVersionMask is the mask for the message version bits (bits 4-7).
	FlagVersionMask byte = 0xF0
)

// DSIZ values for destination node ID encoding.
const (
	DSIZNone   byte = 0x00 // No destination node ID present
	DSIZNodeID byte = 0x01 // 64-bit destination node ID
	DSIZGroup  byte = 0x02 // 16-bit group ID
)

// Security flag bits (byte 3 of the message header).
const (
	// SecFlagSessionTypeMask is the mask for session type bits (bits 0-1).
	SecFlagSessionTypeMask byte = 0x03
	// SecFlagMX indicates a message extension is present (bit 3).
	SecFlagMX byte = 1 << 3
	// SecFlagC indicates the message is encrypted with a privacy key (bit 5).
	SecFlagC byte = 1 << 5
	// SecFlagP indicates the message header is privacy-encrypted (bit 7).
	SecFlagP byte = 1 << 7
)

// Session type values.
const (
	SessionTypeUnicast byte = 0x00
	SessionTypeGroup   byte = 0x01
)

// MessageHeader represents the unencrypted portion of a Matter message.
type MessageHeader struct {
	// Flags is the message flags byte.
	Flags byte
	// SessionID is the session identifier.
	SessionID uint16
	// SecurityFlags is the security flags byte.
	SecurityFlags byte
	// MessageCounter is the monotonically increasing message counter.
	MessageCounter uint32
	// SourceNodeID is the optional source node ID (present if FlagSourceNodeID is set).
	SourceNodeID uint64
	// HasSourceNodeID indicates whether SourceNodeID is present.
	HasSourceNodeID bool
	// DestinationNodeID is the optional 64-bit destination node ID.
	DestinationNodeID uint64
	// HasDestinationNodeID indicates whether DestinationNodeID is present.
	HasDestinationNodeID bool
	// DestinationGroupID is the optional 16-bit destination group ID.
	DestinationGroupID uint16
	// HasDestinationGroupID indicates whether DestinationGroupID is present.
	HasDestinationGroupID bool
}

// Version returns the message version from the flags byte.
func (h *MessageHeader) Version() byte {
	return (h.Flags & FlagVersionMask) >> 4
}

// SessionType returns the session type from the security flags.
func (h *MessageHeader) SessionType() byte {
	return h.SecurityFlags & SecFlagSessionTypeMask
}

// EncodeMessageHeader serializes a MessageHeader into binary form.
func EncodeMessageHeader(h *MessageHeader) ([]byte, error) {
	// Calculate size: 8 bytes minimum (flags + sessionID + secFlags + counter)
	size := 8
	if h.HasSourceNodeID {
		size += 8
	}
	switch {
	case h.HasDestinationNodeID:
		size += 8
	case h.HasDestinationGroupID:
		size += 2
	}

	buf := make([]byte, size)
	offset := 0

	// Byte 0: Message Flags
	flags := h.Flags & FlagVersionMask // preserve version bits
	if h.HasSourceNodeID {
		flags |= FlagSourceNodeID
	}
	switch {
	case h.HasDestinationNodeID:
		flags |= DSIZNodeID
	case h.HasDestinationGroupID:
		flags |= DSIZGroup
	}
	buf[offset] = flags
	offset++

	// Bytes 1-2: Session ID (LE)
	binary.LittleEndian.PutUint16(buf[offset:], h.SessionID)
	offset += 2

	// Byte 3: Security Flags
	buf[offset] = h.SecurityFlags
	offset++

	// Bytes 4-7: Message Counter (LE)
	binary.LittleEndian.PutUint32(buf[offset:], h.MessageCounter)
	offset += 4

	// Optional Source Node ID
	if h.HasSourceNodeID {
		binary.LittleEndian.PutUint64(buf[offset:], h.SourceNodeID)
		offset += 8
	}

	// Optional Destination Node ID / Group ID
	switch {
	case h.HasDestinationNodeID:
		binary.LittleEndian.PutUint64(buf[offset:], h.DestinationNodeID)
	case h.HasDestinationGroupID:
		binary.LittleEndian.PutUint16(buf[offset:], h.DestinationGroupID)
	}

	return buf, nil
}

// DecodeMessageHeader deserializes a MessageHeader from binary data.
// It returns the header and the number of bytes consumed.
func DecodeMessageHeader(data []byte) (*MessageHeader, int, error) {
	if len(data) < 8 {
		return nil, 0, fmt.Errorf("protocol: message header too short: need 8 bytes, got %d", len(data))
	}

	h := &MessageHeader{}
	offset := 0

	// Byte 0: Message Flags
	h.Flags = data[offset]
	offset++

	// Bytes 1-2: Session ID
	h.SessionID = binary.LittleEndian.Uint16(data[offset:])
	offset += 2

	// Byte 3: Security Flags
	h.SecurityFlags = data[offset]
	offset++

	// Bytes 4-7: Message Counter
	h.MessageCounter = binary.LittleEndian.Uint32(data[offset:])
	offset += 4

	// Optional Source Node ID
	if h.Flags&FlagSourceNodeID != 0 {
		if len(data) < offset+8 {
			return nil, 0, fmt.Errorf("protocol: message too short for source node ID")
		}
		h.SourceNodeID = binary.LittleEndian.Uint64(data[offset:])
		h.HasSourceNodeID = true
		offset += 8
	}

	// Optional Destination
	dsiz := h.Flags & FlagDSIZMask
	switch dsiz {
	case DSIZNodeID:
		if len(data) < offset+8 {
			return nil, 0, fmt.Errorf("protocol: message too short for destination node ID")
		}
		h.DestinationNodeID = binary.LittleEndian.Uint64(data[offset:])
		h.HasDestinationNodeID = true
		offset += 8
	case DSIZGroup:
		if len(data) < offset+2 {
			return nil, 0, fmt.Errorf("protocol: message too short for destination group ID")
		}
		h.DestinationGroupID = binary.LittleEndian.Uint16(data[offset:])
		h.HasDestinationGroupID = true
		offset += 2
	case DSIZNone:
		// no destination
	default:
		return nil, 0, fmt.Errorf("protocol: unknown DSIZ value: %d", dsiz)
	}

	return h, offset, nil
}

// Protocol header exchange flag bits.
const (
	// ExFlagInitiator indicates the sender is the exchange initiator (bit 0).
	ExFlagInitiator byte = 1 << 0
	// ExFlagACK indicates an acknowledgment counter is present (bit 1).
	ExFlagACK byte = 1 << 1
	// ExFlagReliable indicates the message requires reliable delivery (bit 2).
	ExFlagReliable byte = 1 << 2
	// ExFlagSecuredExt indicates a secured extension is present (bit 3).
	ExFlagSecuredExt byte = 1 << 3
	// ExFlagVendor indicates a vendor ID is present in the protocol header (bit 4).
	ExFlagVendor byte = 1 << 4
)

// ProtocolHeader represents the protocol header within the decrypted payload.
type ProtocolHeader struct {
	// ExchangeFlags contains the exchange-level flags.
	ExchangeFlags byte
	// ProtocolOpcode is the protocol-specific opcode.
	ProtocolOpcode byte
	// ExchangeID is the exchange identifier.
	ExchangeID uint16
	// ProtocolID identifies the protocol (e.g., 0x0000 for Secure Channel).
	ProtocolID uint16
	// VendorID is the optional vendor ID (present if ExFlagVendor is set).
	VendorID uint16
	// HasVendorID indicates whether VendorID is present.
	HasVendorID bool
	// AckMessageCounter is the optional acknowledgment counter (present if ExFlagACK is set).
	AckMessageCounter uint32
	// HasAckCounter indicates whether AckMessageCounter is present.
	HasAckCounter bool
}

// IsInitiator returns true if the sender is the exchange initiator.
func (p *ProtocolHeader) IsInitiator() bool {
	return p.ExchangeFlags&ExFlagInitiator != 0
}

// NeedsACK returns true if the message requires reliable delivery acknowledgment.
func (p *ProtocolHeader) NeedsACK() bool {
	return p.ExchangeFlags&ExFlagReliable != 0
}

// EncodeProtocolHeader serializes a ProtocolHeader into binary form.
func EncodeProtocolHeader(p *ProtocolHeader) ([]byte, error) {
	size := 6 // flags + opcode + exchangeID + protocolID
	if p.HasVendorID {
		size += 2
	}
	if p.HasAckCounter {
		size += 4
	}

	buf := make([]byte, size)
	offset := 0

	// Build exchange flags
	flags := p.ExchangeFlags
	if p.HasVendorID {
		flags |= ExFlagVendor
	}
	if p.HasAckCounter {
		flags |= ExFlagACK
	}
	buf[offset] = flags
	offset++

	buf[offset] = p.ProtocolOpcode
	offset++

	binary.LittleEndian.PutUint16(buf[offset:], p.ExchangeID)
	offset += 2

	binary.LittleEndian.PutUint16(buf[offset:], p.ProtocolID)
	offset += 2

	if p.HasVendorID {
		binary.LittleEndian.PutUint16(buf[offset:], p.VendorID)
		offset += 2
	}

	if p.HasAckCounter {
		binary.LittleEndian.PutUint32(buf[offset:], p.AckMessageCounter)
	}

	return buf, nil
}

// DecodeProtocolHeader deserializes a ProtocolHeader from binary data.
// It returns the header and the number of bytes consumed.
func DecodeProtocolHeader(data []byte) (*ProtocolHeader, int, error) {
	if len(data) < 6 {
		return nil, 0, fmt.Errorf("protocol: protocol header too short: need 6 bytes, got %d", len(data))
	}

	p := &ProtocolHeader{}
	offset := 0

	p.ExchangeFlags = data[offset]
	offset++

	p.ProtocolOpcode = data[offset]
	offset++

	p.ExchangeID = binary.LittleEndian.Uint16(data[offset:])
	offset += 2

	p.ProtocolID = binary.LittleEndian.Uint16(data[offset:])
	offset += 2

	if p.ExchangeFlags&ExFlagVendor != 0 {
		if len(data) < offset+2 {
			return nil, 0, fmt.Errorf("protocol: protocol header too short for vendor ID")
		}
		p.VendorID = binary.LittleEndian.Uint16(data[offset:])
		p.HasVendorID = true
		offset += 2
	}

	if p.ExchangeFlags&ExFlagACK != 0 {
		if len(data) < offset+4 {
			return nil, 0, fmt.Errorf("protocol: protocol header too short for ack counter")
		}
		p.AckMessageCounter = binary.LittleEndian.Uint32(data[offset:])
		p.HasAckCounter = true
		offset += 4
	}

	return p, offset, nil
}

// Message represents a fully decoded Matter message with both headers and payload.
type Message struct {
	// Header is the unencrypted message header.
	Header MessageHeader
	// Protocol is the protocol header from within the decrypted payload.
	Protocol ProtocolHeader
	// Payload is the application payload (after the protocol header).
	Payload []byte
}
