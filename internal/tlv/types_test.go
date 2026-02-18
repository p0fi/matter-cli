// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import "testing"

func TestElementType_String(t *testing.T) {
	tests := []struct {
		name string
		et   ElementType
		want string
	}{
		{"SignedInt8", TypeSignedInt8, "SignedInt8"},
		{"SignedInt16", TypeSignedInt16, "SignedInt16"},
		{"SignedInt32", TypeSignedInt32, "SignedInt32"},
		{"SignedInt64", TypeSignedInt64, "SignedInt64"},
		{"UnsignedInt8", TypeUnsignedInt8, "UnsignedInt8"},
		{"UnsignedInt16", TypeUnsignedInt16, "UnsignedInt16"},
		{"UnsignedInt32", TypeUnsignedInt32, "UnsignedInt32"},
		{"UnsignedInt64", TypeUnsignedInt64, "UnsignedInt64"},
		{"BoolFalse", TypeBoolFalse, "BoolFalse"},
		{"BoolTrue", TypeBoolTrue, "BoolTrue"},
		{"Float32", TypeFloat32, "Float32"},
		{"Float64", TypeFloat64, "Float64"},
		{"UTF8String1", TypeUTF8String1, "UTF8String1"},
		{"UTF8String2", TypeUTF8String2, "UTF8String2"},
		{"UTF8String4", TypeUTF8String4, "UTF8String4"},
		{"UTF8String8", TypeUTF8String8, "UTF8String8"},
		{"OctetString1", TypeOctetString1, "OctetString1"},
		{"OctetString2", TypeOctetString2, "OctetString2"},
		{"OctetString4", TypeOctetString4, "OctetString4"},
		{"OctetString8", TypeOctetString8, "OctetString8"},
		{"Null", TypeNull, "Null"},
		{"Structure", TypeStructure, "Structure"},
		{"Array", TypeArray, "Array"},
		{"List", TypeList, "List"},
		{"EndOfContainer", TypeEndOfContainer, "EndOfContainer"},
		{"Unknown", ElementType(0x1F), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.et.String(); got != tt.want {
				t.Errorf("ElementType(%02x).String() = %q, want %q", tt.et, got, tt.want)
			}
		})
	}
}

func TestTagConstructors(t *testing.T) {
	t.Run("AnonymousTag", func(t *testing.T) {
		tag := AnonymousTag()
		if tag.Type != TagAnonymous {
			t.Errorf("AnonymousTag().Type = %v, want %v", tag.Type, TagAnonymous)
		}
		if tag.TagNum != 0 {
			t.Errorf("AnonymousTag().TagNum = %v, want 0", tag.TagNum)
		}
	})

	t.Run("ContextTag", func(t *testing.T) {
		tag := ContextTag(42)
		if tag.Type != TagContextSpecific {
			t.Errorf("ContextTag(42).Type = %v, want %v", tag.Type, TagContextSpecific)
		}
		if tag.TagNum != 42 {
			t.Errorf("ContextTag(42).TagNum = %v, want 42", tag.TagNum)
		}
	})

	t.Run("CommonProfileTag2", func(t *testing.T) {
		tag := CommonProfileTag2(0x1234)
		if tag.Type != TagCommonProfile2 {
			t.Errorf("Type = %v, want %v", tag.Type, TagCommonProfile2)
		}
		if tag.TagNum != 0x1234 {
			t.Errorf("TagNum = %v, want %v", tag.TagNum, 0x1234)
		}
	})

	t.Run("CommonProfileTag4", func(t *testing.T) {
		tag := CommonProfileTag4(0x12345678)
		if tag.Type != TagCommonProfile4 {
			t.Errorf("Type = %v, want %v", tag.Type, TagCommonProfile4)
		}
		if tag.TagNum != 0x12345678 {
			t.Errorf("TagNum = %v, want %v", tag.TagNum, 0x12345678)
		}
	})
}

func TestControlByteMasks(t *testing.T) {
	// Verify that element type mask and tag type mask are complementary.
	if tagTypeMask|elementTypeMask != 0xFF {
		t.Errorf("tagTypeMask | elementTypeMask = %02x, want 0xFF", tagTypeMask|elementTypeMask)
	}
	if tagTypeMask&elementTypeMask != 0x00 {
		t.Errorf("tagTypeMask & elementTypeMask = %02x, want 0x00", tagTypeMask&elementTypeMask)
	}
}
