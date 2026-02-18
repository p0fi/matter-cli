// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Reader decodes TLV elements from an io.Reader one element at a time.
type Reader struct {
	r          io.Reader
	containers []ElementType // stack of open container types
	elem       Element       // most recently read element
	done       bool          // true after io.EOF
}

// NewReader returns a new Reader that reads from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

// Next reads the next TLV element. It returns io.EOF when no more elements
// are available. After Next returns successfully, the element is available
// via the Element, Tag, Type, and value accessor methods.
func (r *Reader) Next() error {
	if r.done {
		return io.EOF
	}

	// Read control byte.
	var cb [1]byte
	if _, err := io.ReadFull(r.r, cb[:]); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			r.done = true
			return io.EOF
		}
		return fmt.Errorf("reading control byte: %w", err)
	}

	tagType := TagType(cb[0] & tagTypeMask)
	elemType := ElementType(cb[0] & elementTypeMask)

	// Read tag.
	tag, err := decodeTag(r.r, tagType)
	if err != nil {
		return fmt.Errorf("decoding tag: %w", err)
	}

	// Handle end of container.
	if elemType == TypeEndOfContainer {
		if len(r.containers) == 0 {
			return fmt.Errorf("unexpected end of container: %w", ErrContainerMismatch)
		}
		r.containers = r.containers[:len(r.containers)-1]
		r.elem = Element{Tag: tag, Type: elemType}
		return nil
	}

	// Handle container open.
	if elemType == TypeStructure || elemType == TypeArray || elemType == TypeList {
		r.containers = append(r.containers, elemType)
		r.elem = Element{Tag: tag, Type: elemType}
		return nil
	}

	// Read value.
	val, err := r.readValue(elemType)
	if err != nil {
		return fmt.Errorf("reading value for %s: %w", elemType, err)
	}

	r.elem = Element{Tag: tag, Type: elemType, Value: val}
	return nil
}

// Element returns the most recently read element.
func (r *Reader) Element() Element {
	return r.elem
}

// TagValue returns the tag of the current element.
func (r *Reader) TagValue() Tag {
	return r.elem.Tag
}

// Type returns the type of the current element.
func (r *Reader) Type() ElementType {
	return r.elem.Type
}

// Value returns the value of the current element.
func (r *Reader) Value() any {
	return r.elem.Value
}

// ContainerDepth returns the current nesting depth of open containers.
func (r *Reader) ContainerDepth() int {
	return len(r.containers)
}

func (r *Reader) readValue(elemType ElementType) (any, error) {
	switch elemType {
	case TypeSignedInt8:
		return r.readSignedInt(1)
	case TypeSignedInt16:
		return r.readSignedInt(2)
	case TypeSignedInt32:
		return r.readSignedInt(4)
	case TypeSignedInt64:
		return r.readSignedInt(8)
	case TypeUnsignedInt8:
		return r.readUnsignedInt(1)
	case TypeUnsignedInt16:
		return r.readUnsignedInt(2)
	case TypeUnsignedInt32:
		return r.readUnsignedInt(4)
	case TypeUnsignedInt64:
		return r.readUnsignedInt(8)
	case TypeBoolFalse:
		return false, nil
	case TypeBoolTrue:
		return true, nil
	case TypeFloat32:
		return r.readFloat32()
	case TypeFloat64:
		return r.readFloat64()
	case TypeUTF8String1:
		return r.readUTF8String(1)
	case TypeUTF8String2:
		return r.readUTF8String(2)
	case TypeUTF8String4:
		return r.readUTF8String(4)
	case TypeUTF8String8:
		return r.readUTF8String(8)
	case TypeOctetString1:
		return r.readOctetString(1)
	case TypeOctetString2:
		return r.readOctetString(2)
	case TypeOctetString4:
		return r.readOctetString(4)
	case TypeOctetString8:
		return r.readOctetString(8)
	case TypeNull:
		return nil, nil
	default:
		return nil, fmt.Errorf("element type %02x: %w", elemType, ErrInvalidType)
	}
}

func (r *Reader) readSignedInt(size int) (int64, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return 0, fmt.Errorf("reading %d-byte signed int: %w", size, err)
	}
	switch size {
	case 1:
		return int64(int8(buf[0])), nil
	case 2:
		return int64(int16(binary.LittleEndian.Uint16(buf))), nil
	case 4:
		return int64(int32(binary.LittleEndian.Uint32(buf))), nil
	case 8:
		return int64(binary.LittleEndian.Uint64(buf)), nil
	}
	return 0, nil
}

func (r *Reader) readUnsignedInt(size int) (uint64, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return 0, fmt.Errorf("reading %d-byte unsigned int: %w", size, err)
	}
	switch size {
	case 1:
		return uint64(buf[0]), nil
	case 2:
		return uint64(binary.LittleEndian.Uint16(buf)), nil
	case 4:
		return uint64(binary.LittleEndian.Uint32(buf)), nil
	case 8:
		return binary.LittleEndian.Uint64(buf), nil
	}
	return 0, nil
}

func (r *Reader) readFloat32() (float32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r.r, buf[:]); err != nil {
		return 0, fmt.Errorf("reading float32: %w", err)
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(buf[:])), nil
}

func (r *Reader) readFloat64() (float64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r.r, buf[:]); err != nil {
		return 0, fmt.Errorf("reading float64: %w", err)
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(buf[:])), nil
}

func (r *Reader) readStringLength(lenBytes int) (int, error) {
	buf := make([]byte, lenBytes)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return 0, fmt.Errorf("reading string length: %w", err)
	}
	switch lenBytes {
	case 1:
		return int(buf[0]), nil
	case 2:
		return int(binary.LittleEndian.Uint16(buf)), nil
	case 4:
		return int(binary.LittleEndian.Uint32(buf)), nil
	case 8:
		return int(binary.LittleEndian.Uint64(buf)), nil
	}
	return 0, nil
}

func (r *Reader) readUTF8String(lenBytes int) (string, error) {
	n, err := r.readStringLength(lenBytes)
	if err != nil {
		return "", err
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return "", fmt.Errorf("reading UTF-8 string data: %w", err)
	}
	return string(data), nil
}

func (r *Reader) readOctetString(lenBytes int) ([]byte, error) {
	n, err := r.readStringLength(lenBytes)
	if err != nil {
		return nil, err
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r.r, data); err != nil {
		return nil, fmt.Errorf("reading octet string data: %w", err)
	}
	return data, nil
}
