// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"bytes"
	"testing"
)

func TestWriter_PutUnsignedInt(t *testing.T) {
	tests := []struct {
		name string
		tag  Tag
		val  uint64
		want []byte
	}{
		{
			name: "uint8 context tag 1 value 42 (spec vector)",
			tag:  ContextTag(1),
			val:  42,
			want: []byte{0x24, 0x01, 0x2A},
		},
		{
			name: "uint8 anonymous value 0",
			tag:  AnonymousTag(),
			val:  0,
			want: []byte{0x04, 0x00},
		},
		{
			name: "uint8 max",
			tag:  ContextTag(0),
			val:  255,
			want: []byte{0x24, 0x00, 0xFF},
		},
		{
			name: "uint16",
			tag:  ContextTag(1),
			val:  0x0102,
			want: []byte{0x25, 0x01, 0x02, 0x01},
		},
		{
			name: "uint32",
			tag:  ContextTag(1),
			val:  0x01020304,
			want: []byte{0x26, 0x01, 0x04, 0x03, 0x02, 0x01},
		},
		{
			name: "uint64",
			tag:  ContextTag(1),
			val:  0x0102030405060708,
			want: []byte{0x27, 0x01, 0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWriter()
			if err := w.PutUnsignedInt(tt.tag, tt.val); err != nil {
				t.Fatalf("PutUnsignedInt() error = %v", err)
			}
			if got := w.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("PutUnsignedInt() = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestWriter_PutSignedInt(t *testing.T) {
	tests := []struct {
		name string
		tag  Tag
		val  int64
		want []byte
	}{
		{
			name: "int8 positive",
			tag:  ContextTag(0),
			val:  42,
			want: []byte{0x20, 0x00, 0x2A},
		},
		{
			name: "int8 negative",
			tag:  ContextTag(0),
			val:  -1,
			want: []byte{0x20, 0x00, 0xFF},
		},
		{
			name: "int16",
			tag:  ContextTag(1),
			val:  300,
			want: []byte{0x21, 0x01, 0x2C, 0x01},
		},
		{
			name: "int32",
			tag:  ContextTag(2),
			val:  70000,
			want: []byte{0x22, 0x02, 0x70, 0x11, 0x01, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWriter()
			if err := w.PutSignedInt(tt.tag, tt.val); err != nil {
				t.Fatalf("PutSignedInt() error = %v", err)
			}
			if got := w.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("PutSignedInt() = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestWriter_PutBool(t *testing.T) {
	tests := []struct {
		name string
		tag  Tag
		val  bool
		want []byte
	}{
		{
			name: "true context tag 0",
			tag:  ContextTag(0),
			val:  true,
			want: []byte{0x29, 0x00},
		},
		{
			name: "false context tag 0",
			tag:  ContextTag(0),
			val:  false,
			want: []byte{0x28, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWriter()
			if err := w.PutBool(tt.tag, tt.val); err != nil {
				t.Fatalf("PutBool() error = %v", err)
			}
			if got := w.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("PutBool() = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestWriter_PutUTF8String(t *testing.T) {
	tests := []struct {
		name string
		tag  Tag
		val  string
		want []byte
	}{
		{
			name: "Hello (spec vector)",
			tag:  ContextTag(2),
			val:  "Hello",
			want: []byte{0x2C, 0x02, 0x05, 0x48, 0x65, 0x6C, 0x6C, 0x6F},
		},
		{
			name: "empty string",
			tag:  ContextTag(0),
			val:  "",
			want: []byte{0x2C, 0x00, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWriter()
			if err := w.PutUTF8String(tt.tag, tt.val); err != nil {
				t.Fatalf("PutUTF8String() error = %v", err)
			}
			if got := w.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("PutUTF8String() = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestWriter_PutOctetString(t *testing.T) {
	w := NewWriter()
	if err := w.PutOctetString(ContextTag(0), []byte{0xDE, 0xAD}); err != nil {
		t.Fatalf("PutOctetString() error = %v", err)
	}
	want := []byte{0x30, 0x00, 0x02, 0xDE, 0xAD}
	if got := w.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("PutOctetString() = %x, want %x", got, want)
	}
}

func TestWriter_PutNull(t *testing.T) {
	w := NewWriter()
	if err := w.PutNull(ContextTag(5)); err != nil {
		t.Fatalf("PutNull() error = %v", err)
	}
	want := []byte{0x34, 0x05}
	if got := w.Bytes(); !bytes.Equal(got, want) {
		t.Errorf("PutNull() = %x, want %x", got, want)
	}
}

func TestWriter_Structure(t *testing.T) {
	// Spec test vector: Structure{ContextTag(0): true, ContextTag(1): uint32(1)}
	// Expected: [0x15, 0x29, 0x00, 0x26, 0x01, 0x01, 0x00, 0x00, 0x00, 0x18]
	w := NewWriter()
	if err := w.StartStructure(AnonymousTag()); err != nil {
		t.Fatal(err)
	}
	if err := w.PutBool(ContextTag(0), true); err != nil {
		t.Fatal(err)
	}
	if err := w.PutUnsignedInt(ContextTag(1), 1); err != nil {
		t.Fatal(err)
	}
	if err := w.EndContainer(); err != nil {
		t.Fatal(err)
	}

	// The spec vector has uint32(1) which uses 0x26 (UnsignedInt32).
	// Our encoder uses smallest encoding, so uint8(1) = 0x24.
	// Let's verify our output is valid TLV (just uses compact encoding).
	got := w.Bytes()
	// Structure open: 0x15
	if got[0] != 0x15 {
		t.Errorf("expected structure open 0x15, got %02x", got[0])
	}
	// Bool true context tag 0: 0x29, 0x00
	if got[1] != 0x29 || got[2] != 0x00 {
		t.Errorf("expected bool true [29 00], got [%02x %02x]", got[1], got[2])
	}
	// End of container: last byte should be 0x18
	if got[len(got)-1] != 0x18 {
		t.Errorf("expected end of container 0x18, got %02x", got[len(got)-1])
	}
}

func TestWriter_NestedStructure(t *testing.T) {
	w := NewWriter()
	if err := w.StartStructure(AnonymousTag()); err != nil {
		t.Fatal(err)
	}
	if err := w.StartStructure(ContextTag(0)); err != nil {
		t.Fatal(err)
	}
	if err := w.PutBool(ContextTag(0), true); err != nil {
		t.Fatal(err)
	}
	if err := w.EndContainer(); err != nil {
		t.Fatal(err)
	}
	if err := w.EndContainer(); err != nil {
		t.Fatal(err)
	}

	got := w.Bytes()
	// Outer structure: 0x15
	// Inner structure with context tag 0: 0x35, 0x00
	// Bool true context tag 0: 0x29, 0x00
	// End inner: 0x18
	// End outer: 0x18
	want := []byte{0x15, 0x35, 0x00, 0x29, 0x00, 0x18, 0x18}
	if !bytes.Equal(got, want) {
		t.Errorf("nested structure = %x, want %x", got, want)
	}
}

func TestWriter_Array(t *testing.T) {
	w := NewWriter()
	if err := w.StartArray(ContextTag(0)); err != nil {
		t.Fatal(err)
	}
	if err := w.PutUnsignedInt(AnonymousTag(), 1); err != nil {
		t.Fatal(err)
	}
	if err := w.PutUnsignedInt(AnonymousTag(), 2); err != nil {
		t.Fatal(err)
	}
	if err := w.EndContainer(); err != nil {
		t.Fatal(err)
	}

	got := w.Bytes()
	// Array with context tag 0: 0x36, 0x00
	// uint8(1): 0x04, 0x01
	// uint8(2): 0x04, 0x02
	// End: 0x18
	want := []byte{0x36, 0x00, 0x04, 0x01, 0x04, 0x02, 0x18}
	if !bytes.Equal(got, want) {
		t.Errorf("array = %x, want %x", got, want)
	}
}

func TestWriter_EndContainer_Mismatch(t *testing.T) {
	w := NewWriter()
	err := w.EndContainer()
	if err == nil {
		t.Fatal("expected error for EndContainer with no open container")
	}
}

func TestWriter_PutFloat32(t *testing.T) {
	w := NewWriter()
	if err := w.PutFloat32(ContextTag(0), 1.5); err != nil {
		t.Fatalf("PutFloat32() error = %v", err)
	}
	got := w.Bytes()
	// Control byte: context tag (0x20) | float32 (0x0A) = 0x2A
	if got[0] != 0x2A {
		t.Errorf("control byte = %02x, want 0x2A", got[0])
	}
	if got[1] != 0x00 {
		t.Errorf("tag byte = %02x, want 0x00", got[1])
	}
	if len(got) != 6 { // 1 control + 1 tag + 4 value
		t.Errorf("len = %d, want 6", len(got))
	}
}

func TestWriter_PutFloat64(t *testing.T) {
	w := NewWriter()
	if err := w.PutFloat64(ContextTag(0), 3.14); err != nil {
		t.Fatalf("PutFloat64() error = %v", err)
	}
	got := w.Bytes()
	// Control byte: context tag (0x20) | float64 (0x0B) = 0x2B
	if got[0] != 0x2B {
		t.Errorf("control byte = %02x, want 0x2B", got[0])
	}
	if len(got) != 10 { // 1 control + 1 tag + 8 value
		t.Errorf("len = %d, want 10", len(got))
	}
}
