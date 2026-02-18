// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"bytes"
	"testing"
)

func TestCodec_UnsecuredRoundTrip(t *testing.T) {
	codec := NewCodec()
	session := &Session{
		ID:   0,
		Type: SessionUnsecured,
	}

	original := &Message{
		Header: MessageHeader{
			SessionID:       0,
			SecurityFlags:   0,
			MessageCounter:  42,
			SourceNodeID:    0x1122334455667788,
			HasSourceNodeID: true,
		},
		Protocol: ProtocolHeader{
			ExchangeFlags:  ExFlagInitiator | ExFlagReliable,
			ProtocolOpcode: 0x20,
			ExchangeID:     1,
			ProtocolID:     0x0000,
		},
		Payload: []byte("hello matter"),
	}

	encoded, err := codec.Encode(original, session)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(encoded, session)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.Header.SessionID != original.Header.SessionID {
		t.Errorf("SessionID: got %d, want %d", decoded.Header.SessionID, original.Header.SessionID)
	}
	if decoded.Header.MessageCounter != original.Header.MessageCounter {
		t.Errorf("MessageCounter: got %d, want %d", decoded.Header.MessageCounter, original.Header.MessageCounter)
	}
	if decoded.Header.SourceNodeID != original.Header.SourceNodeID {
		t.Errorf("SourceNodeID: got %x, want %x", decoded.Header.SourceNodeID, original.Header.SourceNodeID)
	}
	if decoded.Protocol.ExchangeID != original.Protocol.ExchangeID {
		t.Errorf("ExchangeID: got %d, want %d", decoded.Protocol.ExchangeID, original.Protocol.ExchangeID)
	}
	if decoded.Protocol.ProtocolOpcode != original.Protocol.ProtocolOpcode {
		t.Errorf("ProtocolOpcode: got %d, want %d", decoded.Protocol.ProtocolOpcode, original.Protocol.ProtocolOpcode)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload: got %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestCodec_EncryptedRoundTrip(t *testing.T) {
	codec := NewCodec()

	// 16-byte test key.
	key := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}

	session := &Session{
		ID:         1,
		Type:       SessionPASE,
		EncryptKey: key,
		DecryptKey: key, // same key for test (symmetric)
	}

	original := &Message{
		Header: MessageHeader{
			SessionID:       1,
			SecurityFlags:   0,
			MessageCounter:  100,
			SourceNodeID:    0xAABBCCDD,
			HasSourceNodeID: true,
		},
		Protocol: ProtocolHeader{
			ExchangeFlags:  ExFlagInitiator,
			ProtocolOpcode: 0x30,
			ExchangeID:     5,
			ProtocolID:     0x0001,
		},
		Payload: []byte("encrypted payload data here"),
	}

	encoded, err := codec.Encode(original, session)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(encoded, session)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded.Header.MessageCounter != original.Header.MessageCounter {
		t.Errorf("MessageCounter: got %d, want %d", decoded.Header.MessageCounter, original.Header.MessageCounter)
	}
	if decoded.Protocol.ExchangeID != original.Protocol.ExchangeID {
		t.Errorf("ExchangeID: got %d, want %d", decoded.Protocol.ExchangeID, original.Protocol.ExchangeID)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload: got %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestCodec_EncryptedWithWrongKey(t *testing.T) {
	codec := NewCodec()

	key1 := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}
	key2 := []byte{0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8,
		0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0}

	encSession := &Session{
		ID:         1,
		Type:       SessionPASE,
		EncryptKey: key1,
	}
	decSession := &Session{
		ID:         1,
		Type:       SessionPASE,
		DecryptKey: key2,
	}

	msg := &Message{
		Header: MessageHeader{
			SessionID:      1,
			MessageCounter: 1,
		},
		Protocol: ProtocolHeader{
			ExchangeFlags:  ExFlagInitiator,
			ProtocolOpcode: 0x01,
			ExchangeID:     1,
			ProtocolID:     0,
		},
		Payload: []byte("secret"),
	}

	encoded, err := codec.Encode(msg, encSession)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	_, err = codec.Decode(encoded, decSession)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong key")
	}
}

func TestCodec_NilSession(t *testing.T) {
	codec := NewCodec()

	msg := &Message{
		Header: MessageHeader{
			SessionID:      0,
			MessageCounter: 1,
		},
		Protocol: ProtocolHeader{
			ExchangeFlags:  ExFlagInitiator,
			ProtocolOpcode: 0x01,
			ExchangeID:     1,
			ProtocolID:     0,
		},
		Payload: []byte("test"),
	}

	encoded, err := codec.Encode(msg, nil)
	if err != nil {
		t.Fatalf("Encode with nil session: %v", err)
	}

	decoded, err := codec.Decode(encoded, nil)
	if err != nil {
		t.Fatalf("Decode with nil session: %v", err)
	}

	if !bytes.Equal(decoded.Payload, msg.Payload) {
		t.Errorf("Payload: got %q, want %q", decoded.Payload, msg.Payload)
	}
}

func TestCodec_EmptyPayload(t *testing.T) {
	codec := NewCodec()
	session := &Session{ID: 0, Type: SessionUnsecured}

	msg := &Message{
		Header: MessageHeader{
			SessionID:      0,
			MessageCounter: 1,
		},
		Protocol: ProtocolHeader{
			ProtocolOpcode: 0x01,
			ExchangeID:     1,
			ProtocolID:     0,
		},
		Payload: nil,
	}

	encoded, err := codec.Encode(msg, session)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(encoded, session)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(decoded.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestCodec_EncryptedEmptyPayload(t *testing.T) {
	codec := NewCodec()
	key := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10}

	session := &Session{
		ID:         1,
		Type:       SessionCASE,
		EncryptKey: key,
		DecryptKey: key,
	}

	msg := &Message{
		Header: MessageHeader{
			SessionID:      1,
			MessageCounter: 1,
		},
		Protocol: ProtocolHeader{
			ProtocolOpcode: 0x01,
			ExchangeID:     1,
			ProtocolID:     0,
		},
		Payload: nil,
	}

	encoded, err := codec.Encode(msg, session)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := codec.Decode(encoded, session)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(decoded.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestBuildNonce(t *testing.T) {
	nonce := buildNonce(0x05, 0x00000064, 0x0102030405060708)
	if len(nonce) != 13 {
		t.Fatalf("nonce length: got %d, want 13", len(nonce))
	}
	if nonce[0] != 0x05 {
		t.Errorf("nonce[0]: got %02x, want 05", nonce[0])
	}
	// Message counter bytes 1-4 (LE): 0x64 = 100
	if nonce[1] != 0x64 || nonce[2] != 0x00 || nonce[3] != 0x00 || nonce[4] != 0x00 {
		t.Errorf("nonce counter: got %x", nonce[1:5])
	}
}
