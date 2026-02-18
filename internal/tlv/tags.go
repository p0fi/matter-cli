// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"encoding/binary"
	"fmt"
	"io"
)

// encodeTag writes the tag bytes for the given tag to w.
// It does not write the control byte; the caller is responsible for that.
func encodeTag(w io.Writer, tag Tag) error {
	switch tag.Type {
	case TagAnonymous:
		// No tag bytes.
		return nil
	case TagContextSpecific:
		_, err := w.Write([]byte{byte(tag.TagNum)})
		return err
	case TagCommonProfile2, TagImplicitProfile2:
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(tag.TagNum))
		_, err := w.Write(buf[:])
		return err
	case TagCommonProfile4, TagImplicitProfile4:
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], tag.TagNum)
		_, err := w.Write(buf[:])
		return err
	case TagFullyQualified6:
		var buf [6]byte
		binary.LittleEndian.PutUint16(buf[0:2], tag.VendorID)
		binary.LittleEndian.PutUint16(buf[2:4], tag.ProfileNum)
		binary.LittleEndian.PutUint16(buf[4:6], uint16(tag.TagNum))
		_, err := w.Write(buf[:])
		return err
	case TagFullyQualified8:
		var buf [8]byte
		binary.LittleEndian.PutUint16(buf[0:2], tag.VendorID)
		binary.LittleEndian.PutUint16(buf[2:4], tag.ProfileNum)
		binary.LittleEndian.PutUint32(buf[4:8], tag.TagNum)
		_, err := w.Write(buf[:])
		return err
	default:
		return fmt.Errorf("encoding tag type %02x: %w", tag.Type, ErrInvalidTag)
	}
}

// decodeTag reads tag bytes from r based on the tag type extracted from the control byte.
func decodeTag(r io.Reader, tagType TagType) (Tag, error) {
	tag := Tag{Type: tagType}

	switch tagType {
	case TagAnonymous:
		return tag, nil
	case TagContextSpecific:
		var buf [1]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return Tag{}, fmt.Errorf("reading context tag: %w", err)
		}
		tag.TagNum = uint32(buf[0])
		return tag, nil
	case TagCommonProfile2, TagImplicitProfile2:
		var buf [2]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return Tag{}, fmt.Errorf("reading 2-byte tag: %w", err)
		}
		tag.TagNum = uint32(binary.LittleEndian.Uint16(buf[:]))
		return tag, nil
	case TagCommonProfile4, TagImplicitProfile4:
		var buf [4]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return Tag{}, fmt.Errorf("reading 4-byte tag: %w", err)
		}
		tag.TagNum = binary.LittleEndian.Uint32(buf[:])
		return tag, nil
	case TagFullyQualified6:
		var buf [6]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return Tag{}, fmt.Errorf("reading 6-byte tag: %w", err)
		}
		tag.VendorID = binary.LittleEndian.Uint16(buf[0:2])
		tag.ProfileNum = binary.LittleEndian.Uint16(buf[2:4])
		tag.TagNum = uint32(binary.LittleEndian.Uint16(buf[4:6]))
		return tag, nil
	case TagFullyQualified8:
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return Tag{}, fmt.Errorf("reading 8-byte tag: %w", err)
		}
		tag.VendorID = binary.LittleEndian.Uint16(buf[0:2])
		tag.ProfileNum = binary.LittleEndian.Uint16(buf[2:4])
		tag.TagNum = binary.LittleEndian.Uint32(buf[4:8])
		return tag, nil
	default:
		return Tag{}, fmt.Errorf("decoding tag type %02x: %w", tagType, ErrInvalidTag)
	}
}

// tagSize returns the number of tag bytes for the given tag type.
func tagSize(tagType TagType) int {
	switch tagType {
	case TagAnonymous:
		return 0
	case TagContextSpecific:
		return 1
	case TagCommonProfile2, TagImplicitProfile2:
		return 2
	case TagCommonProfile4, TagImplicitProfile4:
		return 4
	case TagFullyQualified6:
		return 6
	case TagFullyQualified8:
		return 8
	default:
		return 0
	}
}
