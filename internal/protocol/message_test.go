// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"testing"
)

func TestMessageHeaderRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		header MessageHeader
	}{
		{
			name: "minimal header",
			header: MessageHeader{
				SessionID:      0x1234,
				SecurityFlags:  0,
				MessageCounter: 42,
			},
		},
		{
			name: "with source node ID",
			header: MessageHeader{
				SessionID:       0xABCD,
				SecurityFlags:   SessionTypeUnicast,
				MessageCounter:  0xDEADBEEF,
				SourceNodeID:    0x0102030405060708,
				HasSourceNodeID: true,
			},
		},
		{
			name: "with destination node ID",
			header: MessageHeader{
				SessionID:            0x0001,
				SecurityFlags:        0,
				MessageCounter:       100,
				DestinationNodeID:    0xFFEEDDCCBBAA9988,
				HasDestinationNodeID: true,
			},
		},
		{
			name: "with destination group ID",
			header: MessageHeader{
				SessionID:             0x0010,
				SecurityFlags:         SessionTypeGroup,
				MessageCounter:        200,
				DestinationGroupID:    0x1234,
				HasDestinationGroupID: true,
			},
		},
		{
			name: "with source and destination node IDs",
			header: MessageHeader{
				SessionID:            0x5678,
				SecurityFlags:        0,
				MessageCounter:       300,
				SourceNodeID:         0x1111111111111111,
				HasSourceNodeID:      true,
				DestinationNodeID:    0x2222222222222222,
				HasDestinationNodeID: true,
			},
		},
		{
			name: "with version bits",
			header: MessageHeader{
				Flags:          0x10, // version 1
				SessionID:      0x0001,
				SecurityFlags:  0,
				MessageCounter: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeMessageHeader(&tt.header)
			if err != nil {
				t.Fatalf("EncodeMessageHeader: %v", err)
			}

			decoded, n, err := DecodeMessageHeader(encoded)
			if err != nil {
				t.Fatalf("DecodeMessageHeader: %v", err)
			}
			if n != len(encoded) {
				t.Errorf("consumed %d bytes, expected %d", n, len(encoded))
			}

			if decoded.SessionID != tt.header.SessionID {
				t.Errorf("SessionID: got %d, want %d", decoded.SessionID, tt.header.SessionID)
			}
			if decoded.MessageCounter != tt.header.MessageCounter {
				t.Errorf("MessageCounter: got %d, want %d", decoded.MessageCounter, tt.header.MessageCounter)
			}
			if decoded.HasSourceNodeID != tt.header.HasSourceNodeID {
				t.Errorf("HasSourceNodeID: got %v, want %v", decoded.HasSourceNodeID, tt.header.HasSourceNodeID)
			}
			if decoded.HasSourceNodeID && decoded.SourceNodeID != tt.header.SourceNodeID {
				t.Errorf("SourceNodeID: got %x, want %x", decoded.SourceNodeID, tt.header.SourceNodeID)
			}
			if decoded.HasDestinationNodeID != tt.header.HasDestinationNodeID {
				t.Errorf("HasDestinationNodeID: got %v, want %v", decoded.HasDestinationNodeID, tt.header.HasDestinationNodeID)
			}
			if decoded.HasDestinationNodeID && decoded.DestinationNodeID != tt.header.DestinationNodeID {
				t.Errorf("DestinationNodeID: got %x, want %x", decoded.DestinationNodeID, tt.header.DestinationNodeID)
			}
			if decoded.HasDestinationGroupID != tt.header.HasDestinationGroupID {
				t.Errorf("HasDestinationGroupID: got %v, want %v", decoded.HasDestinationGroupID, tt.header.HasDestinationGroupID)
			}
			if decoded.HasDestinationGroupID && decoded.DestinationGroupID != tt.header.DestinationGroupID {
				t.Errorf("DestinationGroupID: got %x, want %x", decoded.DestinationGroupID, tt.header.DestinationGroupID)
			}
		})
	}
}

func TestProtocolHeaderRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		header ProtocolHeader
	}{
		{
			name: "minimal",
			header: ProtocolHeader{
				ExchangeFlags:  ExFlagInitiator,
				ProtocolOpcode: 0x20,
				ExchangeID:     1,
				ProtocolID:     0x0000,
			},
		},
		{
			name: "with ACK",
			header: ProtocolHeader{
				ExchangeFlags:     ExFlagACK | ExFlagReliable,
				ProtocolOpcode:    0x01,
				ExchangeID:        42,
				ProtocolID:        0x0001,
				AckMessageCounter: 99,
				HasAckCounter:     true,
			},
		},
		{
			name: "with vendor ID",
			header: ProtocolHeader{
				ExchangeFlags:  ExFlagVendor,
				ProtocolOpcode: 0x10,
				ExchangeID:     100,
				ProtocolID:     0xFFFF,
				VendorID:       0x1234,
				HasVendorID:    true,
			},
		},
		{
			name: "with vendor ID and ACK",
			header: ProtocolHeader{
				ExchangeFlags:     ExFlagVendor | ExFlagACK | ExFlagInitiator,
				ProtocolOpcode:    0x05,
				ExchangeID:        200,
				ProtocolID:        0x0002,
				VendorID:          0x5678,
				HasVendorID:       true,
				AckMessageCounter: 12345,
				HasAckCounter:     true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeProtocolHeader(&tt.header)
			if err != nil {
				t.Fatalf("EncodeProtocolHeader: %v", err)
			}

			decoded, n, err := DecodeProtocolHeader(encoded)
			if err != nil {
				t.Fatalf("DecodeProtocolHeader: %v", err)
			}
			if n != len(encoded) {
				t.Errorf("consumed %d bytes, expected %d", n, len(encoded))
			}

			if decoded.ProtocolOpcode != tt.header.ProtocolOpcode {
				t.Errorf("ProtocolOpcode: got %d, want %d", decoded.ProtocolOpcode, tt.header.ProtocolOpcode)
			}
			if decoded.ExchangeID != tt.header.ExchangeID {
				t.Errorf("ExchangeID: got %d, want %d", decoded.ExchangeID, tt.header.ExchangeID)
			}
			if decoded.ProtocolID != tt.header.ProtocolID {
				t.Errorf("ProtocolID: got %d, want %d", decoded.ProtocolID, tt.header.ProtocolID)
			}
			if decoded.HasVendorID != tt.header.HasVendorID {
				t.Errorf("HasVendorID: got %v, want %v", decoded.HasVendorID, tt.header.HasVendorID)
			}
			if decoded.HasVendorID && decoded.VendorID != tt.header.VendorID {
				t.Errorf("VendorID: got %d, want %d", decoded.VendorID, tt.header.VendorID)
			}
			if decoded.HasAckCounter != tt.header.HasAckCounter {
				t.Errorf("HasAckCounter: got %v, want %v", decoded.HasAckCounter, tt.header.HasAckCounter)
			}
			if decoded.HasAckCounter && decoded.AckMessageCounter != tt.header.AckMessageCounter {
				t.Errorf("AckMessageCounter: got %d, want %d", decoded.AckMessageCounter, tt.header.AckMessageCounter)
			}
		})
	}
}

func TestDecodeMessageHeaderTooShort(t *testing.T) {
	_, _, err := DecodeMessageHeader([]byte{0x00, 0x01, 0x02})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestDecodeProtocolHeaderTooShort(t *testing.T) {
	_, _, err := DecodeProtocolHeader([]byte{0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestMessageHeaderVersion(t *testing.T) {
	h := &MessageHeader{Flags: 0x10}
	if v := h.Version(); v != 1 {
		t.Errorf("Version: got %d, want 1", v)
	}
}

func TestProtocolHeaderFlags(t *testing.T) {
	p := &ProtocolHeader{ExchangeFlags: ExFlagInitiator | ExFlagReliable}
	if !p.IsInitiator() {
		t.Error("expected IsInitiator to be true")
	}
	if !p.NeedsACK() {
		t.Error("expected NeedsACK to be true")
	}

	p2 := &ProtocolHeader{ExchangeFlags: 0}
	if p2.IsInitiator() {
		t.Error("expected IsInitiator to be false")
	}
	if p2.NeedsACK() {
		t.Error("expected NeedsACK to be false")
	}
}

func TestMessageHeaderWithPayloadTrailing(t *testing.T) {
	h := &MessageHeader{
		SessionID:      1,
		MessageCounter: 10,
	}
	encoded, err := EncodeMessageHeader(h)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Append trailing data to simulate a payload.
	withPayload := append(encoded, 0xDE, 0xAD, 0xBE, 0xEF)

	decoded, n, err := DecodeMessageHeader(withPayload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n != len(encoded) {
		t.Errorf("consumed %d bytes, expected %d", n, len(encoded))
	}
	if decoded.SessionID != 1 {
		t.Errorf("SessionID: got %d, want 1", decoded.SessionID)
	}
}
