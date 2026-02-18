// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Writer encodes TLV elements into an internal buffer.
// It supports nested containers (structures, arrays, lists) via
// StartStructure/StartArray/StartList and EndContainer.
type Writer struct {
	buf        bytes.Buffer
	containers []ElementType // stack of open container types
}

// NewWriter returns a new Writer ready for encoding.
func NewWriter() *Writer {
	return &Writer{}
}

// Bytes returns the encoded TLV bytes. The caller must not modify the returned slice.
func (w *Writer) Bytes() []byte {
	return w.buf.Bytes()
}

// writeControlByte writes the control byte combining tag type and element type.
func (w *Writer) writeControlByte(tag Tag, elemType ElementType) error {
	cb := byte(tag.Type) | byte(elemType)
	return w.buf.WriteByte(cb)
}

// writeElement writes a complete control byte + tag for the given element type.
func (w *Writer) writeElement(tag Tag, elemType ElementType) error {
	if err := w.writeControlByte(tag, elemType); err != nil {
		return fmt.Errorf("writing control byte: %w", err)
	}
	if err := encodeTag(&w.buf, tag); err != nil {
		return fmt.Errorf("writing tag: %w", err)
	}
	return nil
}

// PutSignedInt writes a signed integer element with the smallest encoding that fits.
func (w *Writer) PutSignedInt(tag Tag, v int64) error {
	var elemType ElementType
	switch {
	case v >= math.MinInt8 && v <= math.MaxInt8:
		elemType = TypeSignedInt8
	case v >= math.MinInt16 && v <= math.MaxInt16:
		elemType = TypeSignedInt16
	case v >= math.MinInt32 && v <= math.MaxInt32:
		elemType = TypeSignedInt32
	default:
		elemType = TypeSignedInt64
	}
	if err := w.writeElement(tag, elemType); err != nil {
		return err
	}
	return w.writeSignedValue(elemType, v)
}

func (w *Writer) writeSignedValue(elemType ElementType, v int64) error {
	switch elemType {
	case TypeSignedInt8:
		return w.buf.WriteByte(byte(int8(v)))
	case TypeSignedInt16:
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(int16(v)))
		_, err := w.buf.Write(buf[:])
		return err
	case TypeSignedInt32:
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(int32(v)))
		_, err := w.buf.Write(buf[:])
		return err
	case TypeSignedInt64:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(v))
		_, err := w.buf.Write(buf[:])
		return err
	}
	return nil
}

// PutUnsignedInt writes an unsigned integer element with the smallest encoding that fits.
func (w *Writer) PutUnsignedInt(tag Tag, v uint64) error {
	var elemType ElementType
	switch {
	case v <= math.MaxUint8:
		elemType = TypeUnsignedInt8
	case v <= math.MaxUint16:
		elemType = TypeUnsignedInt16
	case v <= math.MaxUint32:
		elemType = TypeUnsignedInt32
	default:
		elemType = TypeUnsignedInt64
	}
	if err := w.writeElement(tag, elemType); err != nil {
		return err
	}
	return w.writeUnsignedValue(elemType, v)
}

func (w *Writer) writeUnsignedValue(elemType ElementType, v uint64) error {
	switch elemType {
	case TypeUnsignedInt8:
		return w.buf.WriteByte(byte(v))
	case TypeUnsignedInt16:
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(v))
		_, err := w.buf.Write(buf[:])
		return err
	case TypeUnsignedInt32:
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(v))
		_, err := w.buf.Write(buf[:])
		return err
	case TypeUnsignedInt64:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], v)
		_, err := w.buf.Write(buf[:])
		return err
	}
	return nil
}

// PutBool writes a boolean element.
func (w *Writer) PutBool(tag Tag, v bool) error {
	elemType := TypeBoolFalse
	if v {
		elemType = TypeBoolTrue
	}
	return w.writeElement(tag, elemType)
}

// PutFloat32 writes a 32-bit floating-point element.
func (w *Writer) PutFloat32(tag Tag, v float32) error {
	if err := w.writeElement(tag, TypeFloat32); err != nil {
		return err
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
	_, err := w.buf.Write(buf[:])
	return err
}

// PutFloat64 writes a 64-bit floating-point element.
func (w *Writer) PutFloat64(tag Tag, v float64) error {
	if err := w.writeElement(tag, TypeFloat64); err != nil {
		return err
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v))
	_, err := w.buf.Write(buf[:])
	return err
}

// PutUTF8String writes a UTF-8 string element with the smallest length encoding.
func (w *Writer) PutUTF8String(tag Tag, v string) error {
	length := len(v)
	elemType, err := stringElementType(length, TypeUTF8String1)
	if err != nil {
		return err
	}
	if err := w.writeElement(tag, elemType); err != nil {
		return err
	}
	if err := w.writeLength(elemType, length); err != nil {
		return err
	}
	_, werr := w.buf.WriteString(v)
	return werr
}

// PutOctetString writes an octet (byte) string element with the smallest length encoding.
func (w *Writer) PutOctetString(tag Tag, v []byte) error {
	length := len(v)
	elemType, err := stringElementType(length, TypeOctetString1)
	if err != nil {
		return err
	}
	if err := w.writeElement(tag, elemType); err != nil {
		return err
	}
	if err := w.writeLength(elemType, length); err != nil {
		return err
	}
	_, werr := w.buf.Write(v)
	return werr
}

// stringElementType returns the smallest string element type for the given length.
// base must be TypeUTF8String1 or TypeOctetString1.
func stringElementType(length int, base ElementType) (ElementType, error) {
	switch {
	case length <= math.MaxUint8:
		return base, nil
	case length <= math.MaxUint16:
		return base + 1, nil
	case length <= math.MaxUint32:
		return base + 2, nil
	default:
		return base + 3, nil
	}
}

// writeLength writes the length prefix for a string element.
func (w *Writer) writeLength(elemType ElementType, length int) error {
	// The length size is determined by the element sub-type (1/2/4/8 bytes).
	switch elemType {
	case TypeUTF8String1, TypeOctetString1:
		return w.buf.WriteByte(byte(length))
	case TypeUTF8String2, TypeOctetString2:
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], uint16(length))
		_, err := w.buf.Write(buf[:])
		return err
	case TypeUTF8String4, TypeOctetString4:
		var buf [4]byte
		binary.LittleEndian.PutUint32(buf[:], uint32(length))
		_, err := w.buf.Write(buf[:])
		return err
	case TypeUTF8String8, TypeOctetString8:
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], uint64(length))
		_, err := w.buf.Write(buf[:])
		return err
	default:
		return fmt.Errorf("writing length for type %02x: %w", elemType, ErrInvalidType)
	}
}

// PutNull writes a null element.
func (w *Writer) PutNull(tag Tag) error {
	return w.writeElement(tag, TypeNull)
}

// StartStructure begins a structure container.
func (w *Writer) StartStructure(tag Tag) error {
	if err := w.writeElement(tag, TypeStructure); err != nil {
		return err
	}
	w.containers = append(w.containers, TypeStructure)
	return nil
}

// StartArray begins an array container.
func (w *Writer) StartArray(tag Tag) error {
	if err := w.writeElement(tag, TypeArray); err != nil {
		return err
	}
	w.containers = append(w.containers, TypeArray)
	return nil
}

// StartList begins a list container.
func (w *Writer) StartList(tag Tag) error {
	if err := w.writeElement(tag, TypeList); err != nil {
		return err
	}
	w.containers = append(w.containers, TypeList)
	return nil
}

// PutPreEncodedStruct writes a structure element containing pre-encoded TLV
// field bytes. The data parameter contains the raw TLV-encoded inner fields
// (without the struct-start or end-of-container markers). This bypasses the
// container stack since the raw bytes are trusted.
func (w *Writer) PutPreEncodedStruct(tag Tag, data []byte) error {
	if err := w.writeElement(tag, TypeStructure); err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := w.buf.Write(data); err != nil {
			return err
		}
	}
	return w.buf.WriteByte(byte(TypeEndOfContainer))
}

// EndContainer ends the current open container.
func (w *Writer) EndContainer() error {
	if len(w.containers) == 0 {
		return fmt.Errorf("ending container with no open container: %w", ErrContainerMismatch)
	}
	w.containers = w.containers[:len(w.containers)-1]
	return w.buf.WriteByte(byte(TypeEndOfContainer))
}
