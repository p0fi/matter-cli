// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package crypto

import (
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"math/big"
)

// Matter-specified M and N points for SPAKE2+ on P-256.
// These are the "nothing up my sleeve" generator points from the Matter specification.
var (
	spake2pM *eccPoint
	spake2pN *eccPoint
)

// eccPoint represents a point on the P-256 curve.
type eccPoint struct {
	X, Y *big.Int
}

func init() {
	// M point for SPAKE2+ (from Matter spec, SEC1 uncompressed encoding).
	mxBytes := mustDecodeHex("02886e2f97ace46e55ba9dd7242579f2993b64e16ef3dcab95afd497333d8fa12f")
	mx, my := elliptic.UnmarshalCompressed(elliptic.P256(), mxBytes)
	if mx == nil {
		panic("crypto: invalid SPAKE2+ M point")
	}
	spake2pM = &eccPoint{X: mx, Y: my}

	// N point for SPAKE2+ (from Matter spec, SEC1 uncompressed encoding).
	nxBytes := mustDecodeHex("03d8bbd6c639c62937b04d997f38c3770719c629d7014d49a24b4f98baa1292b49")
	nx, ny := elliptic.UnmarshalCompressed(elliptic.P256(), nxBytes)
	if nx == nil {
		panic("crypto: invalid SPAKE2+ N point")
	}
	spake2pN = &eccPoint{X: nx, Y: ny}
}

// mustDecodeHex decodes a hex string; panics on error.
func mustDecodeHex(s string) []byte {
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		b[i/2] = hexByte(s[i])<<4 | hexByte(s[i+1])
	}
	return b
}

func hexByte(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	default:
		panic("crypto: invalid hex character")
	}
}

// SPAKE2PProver implements the prover (commissioner) side of the SPAKE2+ protocol.
type SPAKE2PProver struct {
	curve elliptic.Curve
	w0    *big.Int
	w1    *big.Int
	x     *big.Int // random scalar
	pA    []byte   // our public share (X = x*G + w0*M), uncompressed
}

// SPAKE2PVerifier implements the verifier (commissionee/device) side of the SPAKE2+ protocol.
type SPAKE2PVerifier struct {
	curve elliptic.Curve
	w0    *big.Int
	lX    *big.Int // L = w1*G point
	lY    *big.Int
	y     *big.Int // random scalar
	pB    []byte   // our public share (Y = y*G + w0*N), uncompressed
}

// NewSPAKE2PProver creates a new SPAKE2+ prover using w0 and w1 as 32-byte big-endian scalars.
func NewSPAKE2PProver(w0, w1 []byte) (*SPAKE2PProver, error) {
	curve := P256()
	n := curve.Params().N

	w0Int := new(big.Int).SetBytes(w0)
	w1Int := new(big.Int).SetBytes(w1)

	if w0Int.Sign() == 0 || w0Int.Cmp(n) >= 0 {
		return nil, fmt.Errorf("crypto: SPAKE2+ w0 out of range")
	}
	if w1Int.Sign() == 0 || w1Int.Cmp(n) >= 0 {
		return nil, fmt.Errorf("crypto: SPAKE2+ w1 out of range")
	}

	return &SPAKE2PProver{
		curve: curve,
		w0:    w0Int,
		w1:    w1Int,
	}, nil
}

// ComputePublicShare computes and returns pA = x*G + w0*M (uncompressed point).
func (p *SPAKE2PProver) ComputePublicShare() ([]byte, error) {
	curve := p.curve
	n := curve.Params().N

	// Generate random scalar x.
	x, err := rand.Int(rand.Reader, n)
	if err != nil {
		return nil, fmt.Errorf("crypto: SPAKE2+ generate random: %w", err)
	}
	if x.Sign() == 0 {
		x.SetInt64(1)
	}
	p.x = x

	// X = x*G + w0*M
	xGx, xGy := curve.ScalarBaseMult(x.Bytes())
	w0Mx, w0My := curve.ScalarMult(spake2pM.X, spake2pM.Y, p.w0.Bytes())
	Xx, Xy := curve.Add(xGx, xGy, w0Mx, w0My)

	p.pA = elliptic.Marshal(curve, Xx, Xy)
	return p.pA, nil
}

// ComputeSecretAndConfirm processes the verifier's public share pB and computes
// the shared secret and confirmation values.
// Returns (Ke, cA, cB) where Ke is the encryption key, cA is our confirmation
// to send, and cB is the expected confirmation from the verifier.
func (p *SPAKE2PProver) ComputeSecretAndConfirm(context, idProver, idVerifier, pB []byte) (Ke, cA, cB []byte, err error) {
	curve := p.curve
	n := curve.Params().N

	// Parse pB.
	Yx, Yy := elliptic.Unmarshal(curve, pB)
	if Yx == nil {
		return nil, nil, nil, fmt.Errorf("crypto: SPAKE2+ invalid pB point")
	}

	// Z = x * (Y - w0*N)
	// V = w1 * (Y - w0*N)
	w0Nx, w0Ny := curve.ScalarMult(spake2pN.X, spake2pN.Y, p.w0.Bytes())
	// Negate: -w0N has Y coordinate = p - y.
	negW0Ny := new(big.Int).Sub(curve.Params().P, w0Ny)
	tmpX, tmpY := curve.Add(Yx, Yy, w0Nx, negW0Ny) // Y - w0*N

	zX, zY := curve.ScalarMult(tmpX, tmpY, p.x.Bytes())
	vX, vY := curve.ScalarMult(tmpX, tmpY, p.w1.Bytes())

	// Cofactor h = 1 for P-256, so no cofactor multiply needed.
	_ = n

	return computeTranscriptAndKeys(curve, context, idProver, idVerifier, p.pA, pB, zX, zY, vX, vY, p.w0)
}

// NewSPAKE2PVerifier creates a new SPAKE2+ verifier using w0 (32-byte scalar) and L (uncompressed point = w1*G).
func NewSPAKE2PVerifier(w0, L []byte) (*SPAKE2PVerifier, error) {
	curve := P256()
	n := curve.Params().N

	w0Int := new(big.Int).SetBytes(w0)
	if w0Int.Sign() == 0 || w0Int.Cmp(n) >= 0 {
		return nil, fmt.Errorf("crypto: SPAKE2+ w0 out of range")
	}

	lx, ly := elliptic.Unmarshal(curve, L)
	if lx == nil {
		return nil, fmt.Errorf("crypto: SPAKE2+ invalid L point")
	}

	return &SPAKE2PVerifier{
		curve: curve,
		w0:    w0Int,
		lX:    lx,
		lY:    ly,
	}, nil
}

// ComputePublicShare computes and returns pB = y*G + w0*N (uncompressed point).
func (v *SPAKE2PVerifier) ComputePublicShare() ([]byte, error) {
	curve := v.curve
	n := curve.Params().N

	y, err := rand.Int(rand.Reader, n)
	if err != nil {
		return nil, fmt.Errorf("crypto: SPAKE2+ generate random: %w", err)
	}
	if y.Sign() == 0 {
		y.SetInt64(1)
	}
	v.y = y

	// Y = y*G + w0*N
	yGx, yGy := curve.ScalarBaseMult(y.Bytes())
	w0Nx, w0Ny := curve.ScalarMult(spake2pN.X, spake2pN.Y, v.w0.Bytes())
	Yx, Yy := curve.Add(yGx, yGy, w0Nx, w0Ny)

	v.pB = elliptic.Marshal(curve, Yx, Yy)
	return v.pB, nil
}

// ComputeSecretAndConfirm processes the prover's public share pA and computes
// the shared secret and confirmation values.
// Returns (Ke, cB, cA) where Ke is the encryption key, cB is our confirmation
// to send, and cA is the expected confirmation from the prover.
func (v *SPAKE2PVerifier) ComputeSecretAndConfirm(context, idProver, idVerifier, pA []byte) (Ke, cB, cA []byte, err error) {
	curve := v.curve

	// Parse pA.
	Xx, Xy := elliptic.Unmarshal(curve, pA)
	if Xx == nil {
		return nil, nil, nil, fmt.Errorf("crypto: SPAKE2+ invalid pA point")
	}

	// Z = y * (X - w0*M)
	w0Mx, w0My := curve.ScalarMult(spake2pM.X, spake2pM.Y, v.w0.Bytes())
	negW0My := new(big.Int).Sub(curve.Params().P, w0My)
	tmpX, tmpY := curve.Add(Xx, Xy, w0Mx, negW0My) // X - w0*M

	zX, zY := curve.ScalarMult(tmpX, tmpY, v.y.Bytes())

	// V = y * L
	vX, vY := curve.ScalarMult(v.lX, v.lY, v.y.Bytes())

	// computeTranscriptAndKeys returns (Ke, cA, cB) — swap to match our
	// return signature of (Ke, cB, cA) so the verifier sends cB and expects cA.
	ke, innerCA, innerCB, innerErr := computeTranscriptAndKeys(curve, context, idProver, idVerifier, pA, v.pB, zX, zY, vX, vY, v.w0)
	return ke, innerCB, innerCA, innerErr
}

// VerifyConfirmation checks the peer's confirmation value.
func VerifyConfirmation(expected, received []byte) bool {
	return subtle.ConstantTimeCompare(expected, received) == 1
}

// computeTranscriptAndKeys builds the TT hash and derives keys.
// Returns (Ke, cA, cB).
func computeTranscriptAndKeys(curve elliptic.Curve, context, idProver, idVerifier, pA, pB []byte, zX, zY, vX, vY *big.Int, w0 *big.Int) (Ke, cA, cB []byte, err error) {
	// Build TT = Hash(context_len || context || idProver_len || idProver || idVerifier_len || idVerifier ||
	//               M_len || M || N_len || N || pA_len || pA || pB_len || pB ||
	//               Z_len || Z || V_len || V || w0_len || w0)
	mBytes := elliptic.Marshal(curve, spake2pM.X, spake2pM.Y)
	nBytes := elliptic.Marshal(curve, spake2pN.X, spake2pN.Y)
	zBytes := elliptic.Marshal(curve, zX, zY)
	vBytes := elliptic.Marshal(curve, vX, vY)
	w0Bytes := zeroPad(w0.Bytes(), 32)

	h := sha256.New()
	writeWithLength(h, context)
	writeWithLength(h, idProver)
	writeWithLength(h, idVerifier)
	writeWithLength(h, mBytes)
	writeWithLength(h, nBytes)
	writeWithLength(h, pA)
	writeWithLength(h, pB)
	writeWithLength(h, zBytes)
	writeWithLength(h, vBytes)
	writeWithLength(h, w0Bytes)

	tt := h.Sum(nil) // 32 bytes

	// Ka = first 16 bytes, Ke = last 16 bytes.
	Ka := tt[:16]
	Ke = make([]byte, 16)
	copy(Ke, tt[16:])

	// KcA || KcB = KDF(nil, Ka, "ConfirmationKeys", 32)
	kcInfo := []byte("ConfirmationKeys")
	kc, err := HKDFSHA256(Ka, nil, kcInfo, 32)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("crypto: SPAKE2+ derive confirmation keys: %w", err)
	}
	KcA := kc[:16]
	KcB := kc[16:]

	// cA = HMAC-SHA256(KcA, pB)
	macA := hmac.New(sha256.New, KcA)
	macA.Write(pB)
	cA = macA.Sum(nil)

	// cB = HMAC-SHA256(KcB, pA)
	macB := hmac.New(sha256.New, KcB)
	macB.Write(pA)
	cB = macB.Sum(nil)

	return Ke, cA, cB, nil
}

// writeWithLength writes a length-prefixed value to the hash.
// Length is encoded as a little-endian uint64.
func writeWithLength(h interface{ Write([]byte) (int, error) }, data []byte) {
	var lenBuf [8]byte
	lenBuf[0] = byte(len(data))
	lenBuf[1] = byte(len(data) >> 8)
	lenBuf[2] = byte(len(data) >> 16)
	lenBuf[3] = byte(len(data) >> 24)
	lenBuf[4] = byte(len(data) >> 32)
	lenBuf[5] = byte(len(data) >> 40)
	lenBuf[6] = byte(len(data) >> 48)
	lenBuf[7] = byte(len(data) >> 56)
	h.Write(lenBuf[:])
	h.Write(data)
}

// ComputeL computes L = w1 * G (the verifier's precomputed public point).
// w1 is a 32-byte big-endian scalar.
func ComputeL(w1 []byte) []byte {
	curve := P256()
	lx, ly := curve.ScalarBaseMult(w1)
	return elliptic.Marshal(curve, lx, ly)
}
