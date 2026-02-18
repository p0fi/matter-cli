// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"bytes"
	"testing"
)

func TestEncodeDecodeTag(t *testing.T) {
	tests := []struct {
		name string
		tag  Tag
		want []byte
	}{
		{
			name: "anonymous",
			tag:  AnonymousTag(),
			want: nil,
		},
		{
			name: "context specific 0",
			tag:  ContextTag(0),
			want: []byte{0x00},
		},
		{
			name: "context specific 1",
			tag:  ContextTag(1),
			want: []byte{0x01},
		},
		{
			name: "context specific 255",
			tag:  ContextTag(255),
			want: []byte{0xFF},
		},
		{
			name: "common profile 2-byte",
			tag:  CommonProfileTag2(0x0102),
			want: []byte{0x02, 0x01},
		},
		{
			name: "common profile 4-byte",
			tag:  CommonProfileTag4(0x01020304),
			want: []byte{0x04, 0x03, 0x02, 0x01},
		},
		{
			name: "fully qualified 6-byte",
			tag:  Tag{Type: TagFullyQualified6, VendorID: 0x0001, ProfileNum: 0x0002, TagNum: 0x0003},
			want: []byte{0x01, 0x00, 0x02, 0x00, 0x03, 0x00},
		},
		{
			name: "fully qualified 8-byte",
			tag:  Tag{Type: TagFullyQualified8, VendorID: 0x0001, ProfileNum: 0x0002, TagNum: 0x00030004},
			want: []byte{0x01, 0x00, 0x02, 0x00, 0x04, 0x00, 0x03, 0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/encode", func(t *testing.T) {
			var buf bytes.Buffer
			if err := encodeTag(&buf, tt.tag); err != nil {
				t.Fatalf("encodeTag() error = %v", err)
			}
			if got := buf.Bytes(); !bytes.Equal(got, tt.want) {
				t.Errorf("encodeTag() = %v, want %v", got, tt.want)
			}
		})

		t.Run(tt.name+"/decode", func(t *testing.T) {
			r := bytes.NewReader(tt.want)
			got, err := decodeTag(r, tt.tag.Type)
			if err != nil {
				t.Fatalf("decodeTag() error = %v", err)
			}
			if got.Type != tt.tag.Type {
				t.Errorf("Type = %v, want %v", got.Type, tt.tag.Type)
			}
			if got.TagNum != tt.tag.TagNum {
				t.Errorf("TagNum = %v, want %v", got.TagNum, tt.tag.TagNum)
			}
			if got.VendorID != tt.tag.VendorID {
				t.Errorf("VendorID = %v, want %v", got.VendorID, tt.tag.VendorID)
			}
			if got.ProfileNum != tt.tag.ProfileNum {
				t.Errorf("ProfileNum = %v, want %v", got.ProfileNum, tt.tag.ProfileNum)
			}
		})
	}
}

func TestTagSize(t *testing.T) {
	tests := []struct {
		name    string
		tagType TagType
		want    int
	}{
		{"anonymous", TagAnonymous, 0},
		{"context", TagContextSpecific, 1},
		{"common 2", TagCommonProfile2, 2},
		{"common 4", TagCommonProfile4, 4},
		{"implicit 2", TagImplicitProfile2, 2},
		{"implicit 4", TagImplicitProfile4, 4},
		{"fully qualified 6", TagFullyQualified6, 6},
		{"fully qualified 8", TagFullyQualified8, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tagSize(tt.tagType); got != tt.want {
				t.Errorf("tagSize(%02x) = %d, want %d", tt.tagType, got, tt.want)
			}
		})
	}
}
