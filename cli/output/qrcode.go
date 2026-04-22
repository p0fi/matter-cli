// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"io"

	"github.com/mdp/qrterminal/v3"
	"rsc.io/qr"
)

// RenderQRCode writes a scannable 2D QR code for payload to w using Unicode
// half-block characters (two QR modules per terminal cell). Returns false when
// no visual QR was emitted — callers may still want to print the textual code.
//
// The QR uses error-correction level M (matches Matter controllers such as
// Apple Home / Google Home which also encode at level M) and includes the
// standard 4-module quiet zone so phone-camera commissioners can lock onto it.
func RenderQRCode(w io.Writer, payload string) bool {
	if payload == "" {
		return false
	}
	cfg := qrterminal.Config{
		Level:      qr.M,
		Writer:     w,
		HalfBlocks: true,
		QuietZone:  2,
		// Half-block glyphs rely on the terminal's own foreground / background
		// colors, so they render correctly under NO_COLOR without further
		// special-casing.
		BlackChar:      qrterminal.BLACK_BLACK,
		WhiteChar:      qrterminal.WHITE_WHITE,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
	}
	qrterminal.GenerateWithConfig(payload, cfg)
	return true
}
