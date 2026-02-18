// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"encoding/binary"
	"fmt"

	"github.com/p0fi/matter-cli/internal/crypto"
)

// Codec handles encoding and decoding of complete Matter messages,
// including encryption and decryption of the payload using AES-CCM.
type Codec struct{}

// NewCodec creates a new Codec.
func NewCodec() *Codec {
	return &Codec{}
}

// Encode serializes a Message into a complete wire-format byte sequence.
// For secured sessions (non-zero session ID), the payload is encrypted
// with AES-128-CCM using the session's encryption key.
func (c *Codec) Encode(msg *Message, session *Session) ([]byte, error) {
	// Encode the protocol header.
	protoBytes, err := EncodeProtocolHeader(&msg.Protocol)
	if err != nil {
		return nil, fmt.Errorf("protocol: encode protocol header: %w", err)
	}

	// Build the plaintext payload: protocol header + application payload.
	plaintext := make([]byte, 0, len(protoBytes)+len(msg.Payload))
	plaintext = append(plaintext, protoBytes...)
	plaintext = append(plaintext, msg.Payload...)

	// Encode the message header.
	headerBytes, err := EncodeMessageHeader(&msg.Header)
	if err != nil {
		return nil, fmt.Errorf("protocol: encode message header: %w", err)
	}

	var payloadBytes []byte

	if session != nil && session.Type != SessionUnsecured && session.EncryptKey != nil {
		// Encrypt payload using AES-CCM.
		// Per Matter spec section 4.6.1.3: for unicast sessions, if the Source Node ID
		// is not present in the header, use the local (sender's) node ID for the nonce.
		nonceNodeID := msg.Header.SourceNodeID
		if !msg.Header.HasSourceNodeID {
			nonceNodeID = session.LocalNodeID
		}
		nonce := buildNonce(msg.Header.SecurityFlags, msg.Header.MessageCounter, nonceNodeID)
		encrypted, err := crypto.AESCCMEncrypt(session.EncryptKey, nonce, plaintext, headerBytes)
		if err != nil {
			return nil, fmt.Errorf("protocol: encrypt payload: %w", err)
		}
		payloadBytes = encrypted
	} else {
		payloadBytes = plaintext
	}

	// Combine header + payload.
	result := make([]byte, 0, len(headerBytes)+len(payloadBytes))
	result = append(result, headerBytes...)
	result = append(result, payloadBytes...)
	return result, nil
}

// Decode parses a complete wire-format Matter message. For secured sessions,
// the session's decryption key is used to decrypt the payload.
func (c *Codec) Decode(data []byte, session *Session) (*Message, error) {
	header, headerLen, err := DecodeMessageHeader(data)
	if err != nil {
		return nil, fmt.Errorf("protocol: decode message header: %w", err)
	}

	encPayload := data[headerLen:]
	headerBytes := data[:headerLen]

	var plaintext []byte

	if session != nil && session.Type != SessionUnsecured && session.DecryptKey != nil {
		// Per Matter spec section 4.6.1.3: for unicast sessions, if the Source Node ID
		// is not present in the header, use the peer's node ID for the nonce.
		nonceNodeID := header.SourceNodeID
		if !header.HasSourceNodeID {
			nonceNodeID = session.PeerNodeID
		}
		nonce := buildNonce(header.SecurityFlags, header.MessageCounter, nonceNodeID)
		plaintext, err = crypto.AESCCMDecrypt(session.DecryptKey, nonce, encPayload, headerBytes)
		if err != nil {
			return nil, fmt.Errorf("protocol: decrypt payload: %w", err)
		}
	} else {
		plaintext = encPayload
	}

	proto, protoLen, err := DecodeProtocolHeader(plaintext)
	if err != nil {
		return nil, fmt.Errorf("protocol: decode protocol header: %w", err)
	}

	msg := &Message{
		Header:   *header,
		Protocol: *proto,
		Payload:  plaintext[protoLen:],
	}
	return msg, nil
}

// buildNonce constructs the 13-byte AES-CCM nonce as specified by Matter:
//
//	nonce = securityFlags (1 byte) || messageCounter (4 bytes LE) || sourceNodeID (8 bytes LE)
func buildNonce(securityFlags byte, messageCounter uint32, sourceNodeID uint64) []byte {
	nonce := make([]byte, crypto.AESCCMNonceSize)
	nonce[0] = securityFlags
	binary.LittleEndian.PutUint32(nonce[1:5], messageCounter)
	binary.LittleEndian.PutUint64(nonce[5:13], sourceNodeID)
	return nonce
}
