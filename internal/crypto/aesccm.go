// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
)

// AESCCMTagSize is the authentication tag size in bytes used by Matter (128 bits).
const AESCCMTagSize = 16

// AESCCMNonceSize is the nonce size in bytes used by Matter (13 bytes / 104 bits).
const AESCCMNonceSize = 13

// aesccmLenFieldSize is the L parameter (number of octets for the message length field).
// With a 13-byte nonce: L = 15 - 13 = 2.
const aesccmLenFieldSize = 15 - AESCCMNonceSize

// AESCCMEncrypt encrypts plaintext using AES-128-CCM with the given key, nonce, and
// additional authenticated data (aad). The nonce must be 13 bytes and the key must be
// 16 bytes. It returns ciphertext || tag (len(plaintext) + AESCCMTagSize bytes).
// Implements RFC 3610.
func AESCCMEncrypt(key, nonce, plaintext, aad []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("crypto: AES-CCM key must be 16 bytes, got %d", len(key))
	}
	if len(nonce) != AESCCMNonceSize {
		return nil, fmt.Errorf("crypto: AES-CCM nonce must be %d bytes, got %d", AESCCMNonceSize, len(nonce))
	}
	if len(plaintext) > (1<<(8*aesccmLenFieldSize))-1 {
		return nil, fmt.Errorf("crypto: plaintext too long for AES-CCM")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-CCM create cipher: %w", err)
	}

	// Step 1: Compute CBC-MAC tag (T).
	tag, err := ccmCBCMAC(block, nonce, plaintext, aad)
	if err != nil {
		return nil, err
	}

	// Step 2: CTR-mode encrypt the plaintext and encrypt the tag.
	out := make([]byte, len(plaintext)+AESCCMTagSize)
	ccmCTR(block, nonce, out[:len(plaintext)], plaintext, tag)
	copy(out[len(plaintext):], tag)

	return out, nil
}

// AESCCMDecrypt decrypts ciphertext produced by AESCCMEncrypt. The input must be
// ciphertext || tag. Returns the plaintext, or an error if authentication fails.
func AESCCMDecrypt(key, nonce, ciphertextWithTag, aad []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("crypto: AES-CCM key must be 16 bytes, got %d", len(key))
	}
	if len(nonce) != AESCCMNonceSize {
		return nil, fmt.Errorf("crypto: AES-CCM nonce must be %d bytes, got %d", AESCCMNonceSize, len(nonce))
	}
	if len(ciphertextWithTag) < AESCCMTagSize {
		return nil, fmt.Errorf("crypto: AES-CCM ciphertext too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-CCM create cipher: %w", err)
	}

	ctLen := len(ciphertextWithTag) - AESCCMTagSize
	ciphertext := ciphertextWithTag[:ctLen]
	encTag := make([]byte, AESCCMTagSize)
	copy(encTag, ciphertextWithTag[ctLen:])

	// Step 1: CTR-mode decrypt the ciphertext and the tag.
	plaintext := make([]byte, ctLen)
	tag := make([]byte, AESCCMTagSize)
	copy(tag, encTag)
	ccmCTR(block, nonce, plaintext, ciphertext, tag)

	// Step 2: Recompute CBC-MAC and verify.
	expectedTag, err := ccmCBCMAC(block, nonce, plaintext, aad)
	if err != nil {
		return nil, err
	}

	if subtle.ConstantTimeCompare(tag, expectedTag) != 1 {
		return nil, fmt.Errorf("crypto: AES-CCM authentication failed")
	}

	return plaintext, nil
}

// ccmCBCMAC computes the CBC-MAC tag per RFC 3610 Section 2.2.
func ccmCBCMAC(block cipher.Block, nonce, plaintext, aad []byte) ([]byte, error) {
	bs := block.BlockSize() // 16

	// Build B_0: flags || nonce || Q (message length encoded in L octets).
	b0 := make([]byte, bs)
	// Flags: bit 6 = Adata (1 if len(aad)>0), bits 5..3 = (t-2)/2, bits 2..0 = L-1.
	flags := byte(aesccmLenFieldSize - 1) // L-1
	flags |= byte((AESCCMTagSize-2)/2) << 3
	if len(aad) > 0 {
		flags |= 1 << 6
	}
	b0[0] = flags
	copy(b0[1:1+AESCCMNonceSize], nonce)
	// Encode message length in the last L bytes, big-endian.
	putLength(b0[1+AESCCMNonceSize:], len(plaintext), aesccmLenFieldSize)

	// CBC-MAC: X_1 = E(K, B_0)
	x := make([]byte, bs)
	block.Encrypt(x, b0)

	// If there is associated data, encode and MAC it.
	if len(aad) > 0 {
		ccmMACAAD(block, x, aad)
	}

	// MAC the plaintext.
	ccmMACData(block, x, plaintext)

	// The tag T is the first AESCCMTagSize bytes of the last X.
	return x[:AESCCMTagSize], nil
}

// ccmMACAAD processes the associated data into the CBC-MAC state.
// Per RFC 3610 Section 2.2, the AAD is prepended with a length encoding.
func ccmMACAAD(block cipher.Block, x []byte, aad []byte) {
	bs := block.BlockSize()

	// Encode AAD length. For 0 < l(a) < 2^16 - 2^8, use 2 bytes.
	var lenEnc []byte
	la := len(aad)
	if la < (1<<16)-(1<<8) {
		lenEnc = []byte{byte(la >> 8), byte(la)}
	} else {
		// For very large AAD: 0xff 0xfe || 4-byte length.
		lenEnc = make([]byte, 6)
		lenEnc[0] = 0xff
		lenEnc[1] = 0xfe
		binary.BigEndian.PutUint32(lenEnc[2:], uint32(la))
	}

	// Concatenate length encoding + AAD, then pad to block boundary.
	data := make([]byte, 0, len(lenEnc)+la)
	data = append(data, lenEnc...)
	data = append(data, aad...)

	// Pad to multiple of block size.
	if rem := len(data) % bs; rem != 0 {
		data = append(data, make([]byte, bs-rem)...)
	}

	// XOR and encrypt blocks.
	for i := 0; i < len(data); i += bs {
		xorBytes(x, x, data[i:i+bs])
		block.Encrypt(x, x)
	}
}

// ccmMACData processes data (plaintext) into the CBC-MAC state.
func ccmMACData(block cipher.Block, x []byte, data []byte) {
	bs := block.BlockSize()
	n := len(data)
	i := 0
	for i+bs <= n {
		xorBytes(x, x, data[i:i+bs])
		block.Encrypt(x, x)
		i += bs
	}
	// Handle final partial block (zero-padded).
	if i < n {
		tmp := make([]byte, bs)
		copy(tmp, data[i:])
		xorBytes(x, x, tmp)
		block.Encrypt(x, x)
	}
}

// ccmCTR performs CTR-mode encryption/decryption and encrypts/decrypts the tag per RFC 3610 Section 2.3.
// A_i counter blocks use flags = L-1 in the low 3 bits, followed by the nonce, then the counter.
// The tag is encrypted with A_0 (counter=0); data uses A_1, A_2, ...
func ccmCTR(block cipher.Block, nonce []byte, dst, src []byte, tag []byte) {
	bs := block.BlockSize()

	// Build A_0.
	a := make([]byte, bs)
	a[0] = byte(aesccmLenFieldSize - 1) // flags = L-1
	copy(a[1:1+AESCCMNonceSize], nonce)
	// Counter bytes are already zero for A_0.

	// Encrypt tag with A_0.
	s0 := make([]byte, bs)
	block.Encrypt(s0, a)
	xorBytes(tag, tag, s0[:AESCCMTagSize])

	// Encrypt/decrypt data with A_1, A_2, ...
	ctr := 1
	sBlock := make([]byte, bs)
	for i := 0; i < len(src); i += bs {
		putLength(a[1+AESCCMNonceSize:], ctr, aesccmLenFieldSize)
		block.Encrypt(sBlock, a)

		end := i + bs
		if end > len(src) {
			end = len(src)
		}
		xorBytes(dst[i:end], src[i:end], sBlock[:end-i])
		ctr++
	}
}

// putLength writes val as a big-endian integer into buf of the given byte length.
func putLength(buf []byte, val int, size int) {
	for i := size - 1; i >= 0; i-- {
		buf[i] = byte(val)
		val >>= 8
	}
}

// xorBytes XORs a and b into dst. Lengths must be compatible (min(len(a), len(b)) >= len(dst)).
func xorBytes(dst, a, b []byte) {
	for i := range dst {
		dst[i] = a[i] ^ b[i]
	}
}
