// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

// Package tlv implements the Matter TLV (Tag-Length-Value) encoding format
// as specified in the Matter specification. It provides low-level Writer/Reader
// types for streaming encoding and decoding, as well as high-level Marshal and
// Unmarshal functions that use struct tags for reflection-based conversion.
package tlv

import "errors"

// ElementType represents the type of a TLV element.
// The lower 5 bits of the control byte encode the element type.
type ElementType uint8

const (
	// TypeSignedInt8 is a signed integer, 1-byte value.
	TypeSignedInt8 ElementType = 0x00
	// TypeSignedInt16 is a signed integer, 2-byte value.
	TypeSignedInt16 ElementType = 0x01
	// TypeSignedInt32 is a signed integer, 4-byte value.
	TypeSignedInt32 ElementType = 0x02
	// TypeSignedInt64 is a signed integer, 8-byte value.
	TypeSignedInt64 ElementType = 0x03
	// TypeUnsignedInt8 is an unsigned integer, 1-byte value.
	TypeUnsignedInt8 ElementType = 0x04
	// TypeUnsignedInt16 is an unsigned integer, 2-byte value.
	TypeUnsignedInt16 ElementType = 0x05
	// TypeUnsignedInt32 is an unsigned integer, 4-byte value.
	TypeUnsignedInt32 ElementType = 0x06
	// TypeUnsignedInt64 is an unsigned integer, 8-byte value.
	TypeUnsignedInt64 ElementType = 0x07
	// TypeBoolFalse is a boolean false value (no value bytes).
	TypeBoolFalse ElementType = 0x08
	// TypeBoolTrue is a boolean true value (no value bytes).
	TypeBoolTrue ElementType = 0x09
	// TypeFloat32 is a IEEE 754 single-precision floating-point number.
	TypeFloat32 ElementType = 0x0A
	// TypeFloat64 is a IEEE 754 double-precision floating-point number.
	TypeFloat64 ElementType = 0x0B
	// TypeUTF8String1 is a UTF-8 string with 1-byte length.
	TypeUTF8String1 ElementType = 0x0C
	// TypeUTF8String2 is a UTF-8 string with 2-byte length.
	TypeUTF8String2 ElementType = 0x0D
	// TypeUTF8String4 is a UTF-8 string with 4-byte length.
	TypeUTF8String4 ElementType = 0x0E
	// TypeUTF8String8 is a UTF-8 string with 8-byte length.
	TypeUTF8String8 ElementType = 0x0F
	// TypeOctetString1 is an octet (byte) string with 1-byte length.
	TypeOctetString1 ElementType = 0x10
	// TypeOctetString2 is an octet (byte) string with 2-byte length.
	TypeOctetString2 ElementType = 0x11
	// TypeOctetString4 is an octet (byte) string with 4-byte length.
	TypeOctetString4 ElementType = 0x12
	// TypeOctetString8 is an octet (byte) string with 8-byte length.
	TypeOctetString8 ElementType = 0x13
	// TypeNull represents a null value (no value bytes).
	TypeNull ElementType = 0x14
	// TypeStructure begins a structure container.
	TypeStructure ElementType = 0x15
	// TypeArray begins an array container.
	TypeArray ElementType = 0x16
	// TypeList begins a list container.
	TypeList ElementType = 0x17
	// TypeEndOfContainer marks the end of a container.
	TypeEndOfContainer ElementType = 0x18
)

// String returns a human-readable name for the element type.
func (t ElementType) String() string {
	switch t {
	case TypeSignedInt8:
		return "SignedInt8"
	case TypeSignedInt16:
		return "SignedInt16"
	case TypeSignedInt32:
		return "SignedInt32"
	case TypeSignedInt64:
		return "SignedInt64"
	case TypeUnsignedInt8:
		return "UnsignedInt8"
	case TypeUnsignedInt16:
		return "UnsignedInt16"
	case TypeUnsignedInt32:
		return "UnsignedInt32"
	case TypeUnsignedInt64:
		return "UnsignedInt64"
	case TypeBoolFalse:
		return "BoolFalse"
	case TypeBoolTrue:
		return "BoolTrue"
	case TypeFloat32:
		return "Float32"
	case TypeFloat64:
		return "Float64"
	case TypeUTF8String1:
		return "UTF8String1"
	case TypeUTF8String2:
		return "UTF8String2"
	case TypeUTF8String4:
		return "UTF8String4"
	case TypeUTF8String8:
		return "UTF8String8"
	case TypeOctetString1:
		return "OctetString1"
	case TypeOctetString2:
		return "OctetString2"
	case TypeOctetString4:
		return "OctetString4"
	case TypeOctetString8:
		return "OctetString8"
	case TypeNull:
		return "Null"
	case TypeStructure:
		return "Structure"
	case TypeArray:
		return "Array"
	case TypeList:
		return "List"
	case TypeEndOfContainer:
		return "EndOfContainer"
	default:
		return "Unknown"
	}
}

// TagType represents the tag form encoded in the upper 3 bits of the control byte.
type TagType uint8

const (
	// TagAnonymous indicates no tag (0 tag bytes).
	TagAnonymous TagType = 0x00
	// TagContextSpecific indicates a context-specific tag (1 tag byte).
	TagContextSpecific TagType = 0x20
	// TagCommonProfile2 indicates a common profile tag with 2-byte tag number.
	TagCommonProfile2 TagType = 0x40
	// TagCommonProfile4 indicates a common profile tag with 4-byte tag number.
	TagCommonProfile4 TagType = 0x60
	// TagImplicitProfile2 indicates an implicit profile tag with 2-byte tag number.
	TagImplicitProfile2 TagType = 0x80
	// TagImplicitProfile4 indicates an implicit profile tag with 4-byte tag number.
	TagImplicitProfile4 TagType = 0xA0
	// TagFullyQualified6 indicates a fully-qualified tag with 6 tag bytes.
	TagFullyQualified6 TagType = 0xC0
	// TagFullyQualified8 indicates a fully-qualified tag with 8 tag bytes.
	TagFullyQualified8 TagType = 0xE0
)

const (
	// tagTypeMask extracts the tag type from the control byte (upper 3 bits).
	tagTypeMask = 0xE0
	// elementTypeMask extracts the element type from the control byte (lower 5 bits).
	elementTypeMask = 0x1F
)

// Tag represents a TLV tag. The zero value is an anonymous tag.
type Tag struct {
	// Type is the tag form.
	Type TagType
	// VendorID is the vendor ID for fully-qualified tags.
	VendorID uint16
	// ProfileNum is the profile number for fully-qualified tags.
	ProfileNum uint16
	// TagNum is the tag number.
	TagNum uint32
}

// AnonymousTag returns an anonymous (untagged) tag.
func AnonymousTag() Tag {
	return Tag{Type: TagAnonymous}
}

// ContextTag returns a context-specific tag with the given tag number (0-255).
func ContextTag(tagNum uint8) Tag {
	return Tag{Type: TagContextSpecific, TagNum: uint32(tagNum)}
}

// CommonProfileTag2 returns a common profile tag with a 2-byte tag number.
func CommonProfileTag2(tagNum uint16) Tag {
	return Tag{Type: TagCommonProfile2, TagNum: uint32(tagNum)}
}

// CommonProfileTag4 returns a common profile tag with a 4-byte tag number.
func CommonProfileTag4(tagNum uint32) Tag {
	return Tag{Type: TagCommonProfile4, TagNum: tagNum}
}

// Element represents a single TLV element with its tag, type, and value.
type Element struct {
	// Tag is the element's tag.
	Tag Tag
	// Type is the element type.
	Type ElementType
	// Value holds the element's value. It is nil for containers and end-of-container.
	// For signed integers: int64
	// For unsigned integers: uint64
	// For booleans: bool
	// For floats: float32 or float64
	// For UTF-8 strings: string
	// For octet strings: []byte
	// For null: nil
	Value any
}

// Predefined errors for TLV operations.
var (
	// ErrInvalidType indicates an unrecognized element type in the control byte.
	ErrInvalidType = errors.New("tlv: invalid element type")
	// ErrUnexpectedEOF indicates the input ended before a complete element was read.
	ErrUnexpectedEOF = errors.New("tlv: unexpected end of input")
	// ErrContainerMismatch indicates an end-of-container was found without a matching open container.
	ErrContainerMismatch = errors.New("tlv: container mismatch")
	// ErrUnsupportedType indicates a Go type that cannot be marshaled to TLV.
	ErrUnsupportedType = errors.New("tlv: unsupported Go type")
	// ErrTagInArray indicates a non-anonymous tag was used inside an array container.
	ErrTagInArray = errors.New("tlv: non-anonymous tag inside array")
	// ErrInvalidTag indicates a malformed or invalid tag encoding.
	ErrInvalidTag = errors.New("tlv: invalid tag")
)
