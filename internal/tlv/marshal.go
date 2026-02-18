// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

// fieldInfo holds parsed struct tag information for a single field.
type fieldInfo struct {
	index   int
	tagNum  uint8
	tlvType string
}

// Marshal encodes a Go struct into TLV bytes. The struct fields must be annotated
// with `tlv:"tagNum,type"` tags, for example `tlv:"1,uint"` or `tlv:"2,utf8"`.
// Supported type specifiers: int, uint, bool, float32, float64, utf8, octets, struct, array, list, null.
// Pointer fields are nullable: a nil pointer is encoded as TLV null.
func Marshal(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("marshaling non-struct type %T: %w", v, ErrUnsupportedType)
	}

	w := NewWriter()
	if err := marshalStruct(w, rv); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

// marshalStruct writes all tagged fields of a struct value as a TLV structure.
func marshalStruct(w *Writer, rv reflect.Value) error {
	if err := w.StartStructure(AnonymousTag()); err != nil {
		return err
	}
	if err := marshalFields(w, rv); err != nil {
		return err
	}
	return w.EndContainer()
}

// marshalFields writes all tagged fields of a struct value (without wrapping container).
func marshalFields(w *Writer, rv reflect.Value) error {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		tagStr := sf.Tag.Get("tlv")
		if tagStr == "" || tagStr == "-" {
			continue
		}

		fi, err := parseTag(tagStr, i)
		if err != nil {
			return fmt.Errorf("parsing tag for field %s: %w", sf.Name, err)
		}

		fv := rv.Field(i)
		tag := ContextTag(fi.tagNum)

		// Handle pointer (optional/nullable) fields.
		// In Matter TLV, nil pointer fields are omitted (not encoded as null)
		// because absent fields indicate "not present" or wildcard, while TLV
		// null has a specific meaning that most receivers reject.
		if fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}

		if err := marshalValue(w, tag, fv, fi.tlvType, sf.Name); err != nil {
			return err
		}
	}
	return nil
}

func marshalValue(w *Writer, tag Tag, fv reflect.Value, tlvType string, fieldName string) error {
	switch tlvType {
	case "int":
		return w.PutSignedInt(tag, fv.Int())
	case "uint":
		return w.PutUnsignedInt(tag, fv.Uint())
	case "bool":
		return w.PutBool(tag, fv.Bool())
	case "float32":
		return w.PutFloat32(tag, float32(fv.Float()))
	case "float64":
		return w.PutFloat64(tag, fv.Float())
	case "utf8":
		return w.PutUTF8String(tag, fv.String())
	case "octets":
		return w.PutOctetString(tag, fv.Bytes())
	case "struct":
		if fv.Kind() != reflect.Struct {
			return fmt.Errorf("field %s: expected struct, got %s: %w", fieldName, fv.Kind(), ErrUnsupportedType)
		}
		if err := w.StartStructure(tag); err != nil {
			return err
		}
		if err := marshalFields(w, fv); err != nil {
			return err
		}
		return w.EndContainer()
	case "array":
		if fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
			return fmt.Errorf("field %s: expected slice/array, got %s: %w", fieldName, fv.Kind(), ErrUnsupportedType)
		}
		if err := w.StartArray(tag); err != nil {
			return err
		}
		if err := marshalSlice(w, fv); err != nil {
			return err
		}
		return w.EndContainer()
	case "list":
		if fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
			return fmt.Errorf("field %s: expected slice/array, got %s: %w", fieldName, fv.Kind(), ErrUnsupportedType)
		}
		if err := w.StartList(tag); err != nil {
			return err
		}
		if err := marshalSlice(w, fv); err != nil {
			return err
		}
		return w.EndContainer()
	case "rawstruct":
		// Pre-encoded TLV struct fields. The []byte contains the raw inner
		// field elements; we wrap them in a Structure container.
		return w.PutPreEncodedStruct(tag, fv.Bytes())
	case "rawtlv":
		// Pre-encoded raw TLV element (anonymous-tagged). Parse it and
		// re-encode with the field's context tag.
		raw := fv.Bytes()
		if len(raw) == 0 {
			return nil
		}
		r := NewReader(bytes.NewReader(raw))
		if err := r.Next(); err != nil {
			return fmt.Errorf("field %s: parsing rawtlv: %w", fieldName, err)
		}
		return reencodeElement(w, tag, r)
	case "liststruct":
		// A Go struct encoded as a TLV List (used for Matter path IBs).
		if fv.Kind() != reflect.Struct {
			return fmt.Errorf("field %s: expected struct, got %s: %w", fieldName, fv.Kind(), ErrUnsupportedType)
		}
		if err := w.StartList(tag); err != nil {
			return err
		}
		if err := marshalFields(w, fv); err != nil {
			return err
		}
		return w.EndContainer()
	case "listarray":
		// An array where struct elements are encoded as TLV Lists instead
		// of Structures. Used for arrays of Matter path IBs (e.g.
		// AttributePathIB, EventPathIB).
		if fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
			return fmt.Errorf("field %s: expected slice/array, got %s: %w", fieldName, fv.Kind(), ErrUnsupportedType)
		}
		if err := w.StartArray(tag); err != nil {
			return err
		}
		if err := marshalSliceAsLists(w, fv); err != nil {
			return err
		}
		return w.EndContainer()
	case "listlist":
		// A TLV List where each struct element is also encoded as a TLV List.
		// Used for Matter WriteRequests (WriteRequestMessage.WriteRequests is
		// a list of AttributeDataIB, where each AttributeDataIB is a List).
		if fv.Kind() != reflect.Slice && fv.Kind() != reflect.Array {
			return fmt.Errorf("field %s: expected slice/array, got %s: %w", fieldName, fv.Kind(), ErrUnsupportedType)
		}
		if err := w.StartList(tag); err != nil {
			return err
		}
		if err := marshalSliceAsLists(w, fv); err != nil {
			return err
		}
		return w.EndContainer()
	default:
		return fmt.Errorf("field %s: unknown TLV type %q: %w", fieldName, tlvType, ErrUnsupportedType)
	}
}

func marshalSlice(w *Writer, rv reflect.Value) error {
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i)
		tag := AnonymousTag()

		// Dereference pointer elements.
		if elem.Kind() == reflect.Ptr {
			if elem.IsNil() {
				if err := w.PutNull(tag); err != nil {
					return err
				}
				continue
			}
			elem = elem.Elem()
		}

		if err := marshalArrayElement(w, tag, elem); err != nil {
			return err
		}
	}
	return nil
}

func marshalArrayElement(w *Writer, tag Tag, elem reflect.Value) error {
	switch elem.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return w.PutSignedInt(tag, elem.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return w.PutUnsignedInt(tag, elem.Uint())
	case reflect.Bool:
		return w.PutBool(tag, elem.Bool())
	case reflect.Float32:
		return w.PutFloat32(tag, float32(elem.Float()))
	case reflect.Float64:
		return w.PutFloat64(tag, elem.Float())
	case reflect.String:
		return w.PutUTF8String(tag, elem.String())
	case reflect.Slice:
		if elem.Type().Elem().Kind() == reflect.Uint8 {
			return w.PutOctetString(tag, elem.Bytes())
		}
		return fmt.Errorf("unsupported slice element type: %w", ErrUnsupportedType)
	case reflect.Struct:
		if err := w.StartStructure(tag); err != nil {
			return err
		}
		if err := marshalFields(w, elem); err != nil {
			return err
		}
		return w.EndContainer()
	default:
		return fmt.Errorf("unsupported array element kind %s: %w", elem.Kind(), ErrUnsupportedType)
	}
}

// marshalSliceAsLists is like marshalSlice but encodes struct elements as TLV
// Lists instead of Structures. Non-struct elements are encoded normally.
func marshalSliceAsLists(w *Writer, rv reflect.Value) error {
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i)
		tag := AnonymousTag()

		if elem.Kind() == reflect.Ptr {
			if elem.IsNil() {
				if err := w.PutNull(tag); err != nil {
					return err
				}
				continue
			}
			elem = elem.Elem()
		}

		if elem.Kind() == reflect.Struct {
			if err := w.StartList(tag); err != nil {
				return err
			}
			if err := marshalFields(w, elem); err != nil {
				return err
			}
			if err := w.EndContainer(); err != nil {
				return err
			}
		} else {
			if err := marshalArrayElement(w, tag, elem); err != nil {
				return err
			}
		}
	}
	return nil
}

// Unmarshal decodes TLV bytes into a Go struct. The struct fields must be annotated
// with `tlv:"tagNum,type"` tags matching the encoded data.
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("unmarshal requires non-nil pointer, got %T: %w", v, ErrUnsupportedType)
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("unmarshal requires pointer to struct, got pointer to %s: %w", rv.Kind(), ErrUnsupportedType)
	}

	r := NewReader(bytes.NewReader(data))

	// Read the opening structure.
	if err := r.Next(); err != nil {
		return fmt.Errorf("reading opening element: %w", err)
	}
	if r.Type() != TypeStructure {
		return fmt.Errorf("expected structure, got %s: %w", r.Type(), ErrInvalidType)
	}

	if err := unmarshalFields(r, rv); err != nil {
		return err
	}

	return nil
}

func unmarshalFields(r *Reader, rv reflect.Value) error {
	fieldMap := buildFieldMap(rv.Type())

	for {
		if err := r.Next(); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading next element: %w", err)
		}

		if r.Type() == TypeEndOfContainer {
			return nil
		}

		tagNum := uint8(r.TagValue().TagNum)
		fi, ok := fieldMap[tagNum]
		if !ok {
			// Skip unknown fields (including containers).
			if err := skipElement(r); err != nil {
				return err
			}
			continue
		}

		fv := rv.Field(fi.index)
		if err := unmarshalValue(r, fv, fi.tlvType); err != nil {
			return fmt.Errorf("unmarshaling field %s: %w", rv.Type().Field(fi.index).Name, err)
		}
	}
}

func unmarshalValue(r *Reader, fv reflect.Value, tlvType string) error {
	// Handle null for pointer fields.
	if r.Type() == TypeNull {
		if fv.Kind() == reflect.Ptr {
			fv.Set(reflect.Zero(fv.Type()))
		}
		return nil
	}

	// If the field is a pointer, allocate and dereference.
	if fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			fv.Set(reflect.New(fv.Type().Elem()))
		}
		fv = fv.Elem()
	}

	switch tlvType {
	case "int":
		val, ok := r.Value().(int64)
		if !ok {
			return fmt.Errorf("expected int64, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetInt(val)
	case "uint":
		val, ok := r.Value().(uint64)
		if !ok {
			return fmt.Errorf("expected uint64, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetUint(val)
	case "bool":
		val, ok := r.Value().(bool)
		if !ok {
			return fmt.Errorf("expected bool, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetBool(val)
	case "float32":
		val, ok := r.Value().(float32)
		if !ok {
			return fmt.Errorf("expected float32, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetFloat(float64(val))
	case "float64":
		val, ok := r.Value().(float64)
		if !ok {
			return fmt.Errorf("expected float64, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetFloat(val)
	case "utf8":
		val, ok := r.Value().(string)
		if !ok {
			return fmt.Errorf("expected string, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetString(val)
	case "octets":
		val, ok := r.Value().([]byte)
		if !ok {
			return fmt.Errorf("expected []byte, got %T: %w", r.Value(), ErrInvalidType)
		}
		fv.SetBytes(val)
	case "struct":
		return unmarshalFields(r, fv)
	case "rawstruct":
		// Read the struct body and capture inner fields as raw TLV bytes.
		if r.Type() != TypeStructure {
			return fmt.Errorf("expected structure for rawstruct, got %s: %w", r.Type(), ErrInvalidType)
		}
		raw, captureErr := captureStructBody(r)
		if captureErr != nil {
			return captureErr
		}
		fv.SetBytes(raw)
	case "liststruct":
		// Accept both List and Structure on the wire.
		if r.Type() != TypeList && r.Type() != TypeStructure {
			return fmt.Errorf("expected list/structure for liststruct, got %s: %w", r.Type(), ErrInvalidType)
		}
		return unmarshalFields(r, fv)
	case "array", "list", "listarray", "listlist":
		return unmarshalSlice(r, fv)
	case "rawtlv":
		// Capture any TLV element (scalar or container) as raw bytes.
		raw, captureErr := captureElement(r)
		if captureErr != nil {
			return captureErr
		}
		fv.SetBytes(raw)
	default:
		return fmt.Errorf("unknown TLV type %q: %w", tlvType, ErrUnsupportedType)
	}
	return nil
}

func unmarshalSlice(r *Reader, fv reflect.Value) error {
	elemType := fv.Type().Elem()

	for {
		if err := r.Next(); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("reading array element: %w", err)
		}

		if r.Type() == TypeEndOfContainer {
			return nil
		}

		// Handle null elements.
		if r.Type() == TypeNull {
			if elemType.Kind() == reflect.Ptr {
				fv.Set(reflect.Append(fv, reflect.Zero(elemType)))
			}
			continue
		}

		var elem reflect.Value
		isPtr := elemType.Kind() == reflect.Ptr
		var actualType reflect.Type
		if isPtr {
			actualType = elemType.Elem()
		} else {
			actualType = elemType
		}

		switch actualType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			val, ok := r.Value().(int64)
			if !ok {
				return fmt.Errorf("expected int64, got %T: %w", r.Value(), ErrInvalidType)
			}
			elem = reflect.New(actualType).Elem()
			elem.SetInt(val)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			val, ok := r.Value().(uint64)
			if !ok {
				return fmt.Errorf("expected uint64, got %T: %w", r.Value(), ErrInvalidType)
			}
			elem = reflect.New(actualType).Elem()
			elem.SetUint(val)
		case reflect.Bool:
			val, ok := r.Value().(bool)
			if !ok {
				return fmt.Errorf("expected bool, got %T: %w", r.Value(), ErrInvalidType)
			}
			elem = reflect.New(actualType).Elem()
			elem.SetBool(val)
		case reflect.Float32:
			val, ok := r.Value().(float32)
			if !ok {
				return fmt.Errorf("expected float32, got %T: %w", r.Value(), ErrInvalidType)
			}
			elem = reflect.New(actualType).Elem()
			elem.SetFloat(float64(val))
		case reflect.Float64:
			val, ok := r.Value().(float64)
			if !ok {
				return fmt.Errorf("expected float64, got %T: %w", r.Value(), ErrInvalidType)
			}
			elem = reflect.New(actualType).Elem()
			elem.SetFloat(val)
		case reflect.String:
			val, ok := r.Value().(string)
			if !ok {
				return fmt.Errorf("expected string, got %T: %w", r.Value(), ErrInvalidType)
			}
			elem = reflect.New(actualType).Elem()
			elem.SetString(val)
		case reflect.Slice:
			if actualType.Elem().Kind() == reflect.Uint8 {
				val, ok := r.Value().([]byte)
				if !ok {
					return fmt.Errorf("expected []byte, got %T: %w", r.Value(), ErrInvalidType)
				}
				elem = reflect.New(actualType).Elem()
				elem.SetBytes(val)
			} else {
				return fmt.Errorf("unsupported slice element type: %w", ErrUnsupportedType)
			}
		case reflect.Struct:
			elem = reflect.New(actualType).Elem()
			if err := unmarshalFields(r, elem); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported array element kind %s: %w", actualType.Kind(), ErrUnsupportedType)
		}

		if isPtr {
			ptr := reflect.New(actualType)
			ptr.Elem().Set(elem)
			fv.Set(reflect.Append(fv, ptr))
		} else {
			fv.Set(reflect.Append(fv, elem))
		}
	}
}

// skipElement skips the current element. For containers, it skips all nested elements.
func skipElement(r *Reader) error {
	elemType := r.Type()
	if elemType != TypeStructure && elemType != TypeArray && elemType != TypeList {
		return nil // scalar elements are already consumed
	}

	depth := 1
	for depth > 0 {
		if err := r.Next(); err != nil {
			return fmt.Errorf("skipping element: %w", err)
		}
		switch r.Type() {
		case TypeStructure, TypeArray, TypeList:
			depth++
		case TypeEndOfContainer:
			depth--
		}
	}
	return nil
}

func parseTag(tagStr string, index int) (fieldInfo, error) {
	parts := strings.SplitN(tagStr, ",", 2)
	if len(parts) != 2 {
		return fieldInfo{}, fmt.Errorf("invalid tlv tag %q: expected \"tagNum,type\"", tagStr)
	}

	tagNum, err := strconv.ParseUint(parts[0], 10, 8)
	if err != nil {
		return fieldInfo{}, fmt.Errorf("parsing tag number %q: %w", parts[0], err)
	}

	return fieldInfo{
		index:   index,
		tagNum:  uint8(tagNum),
		tlvType: parts[1],
	}, nil
}

func buildFieldMap(rt reflect.Type) map[uint8]fieldInfo {
	m := make(map[uint8]fieldInfo)
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		tagStr := sf.Tag.Get("tlv")
		if tagStr == "" || tagStr == "-" {
			continue
		}
		fi, err := parseTag(tagStr, i)
		if err != nil {
			continue
		}
		m[fi.tagNum] = fi
	}
	return m
}

// captureStructBody reads all inner elements of the current struct/list
// container and returns them as raw TLV bytes. The reader must be positioned
// after the container-open element (i.e., Next() returned TypeStructure/TypeList).
func captureStructBody(r *Reader) ([]byte, error) {
	w := NewWriter()
	depth := 1
	for depth > 0 {
		if err := r.Next(); err != nil {
			return nil, fmt.Errorf("reading rawstruct body: %w", err)
		}
		if r.Type() == TypeEndOfContainer {
			depth--
			if depth > 0 {
				_ = w.EndContainer()
			}
			continue
		}
		tag := r.TagValue()
		switch r.Type() {
		case TypeStructure:
			depth++
			_ = w.StartStructure(tag)
		case TypeArray:
			depth++
			_ = w.StartArray(tag)
		case TypeList:
			depth++
			_ = w.StartList(tag)
		default:
			if err := reencodeScalar(w, tag, r); err != nil {
				return nil, err
			}
		}
	}
	return w.Bytes(), nil
}

// reencodeElement writes any TLV element (scalar or container) from the reader
// to the writer with the given tag. For containers, it recursively copies all
// nested elements.
func reencodeElement(w *Writer, tag Tag, r *Reader) error {
	switch r.Type() {
	case TypeStructure:
		if err := w.StartStructure(tag); err != nil {
			return err
		}
		return reencodeContainerBody(w, r)
	case TypeArray:
		if err := w.StartArray(tag); err != nil {
			return err
		}
		return reencodeContainerBody(w, r)
	case TypeList:
		if err := w.StartList(tag); err != nil {
			return err
		}
		return reencodeContainerBody(w, r)
	default:
		return reencodeScalar(w, tag, r)
	}
}

func reencodeContainerBody(w *Writer, r *Reader) error {
	for {
		if err := r.Next(); err != nil {
			return err
		}
		if r.Type() == TypeEndOfContainer {
			return w.EndContainer()
		}
		if err := reencodeElement(w, r.TagValue(), r); err != nil {
			return err
		}
	}
}

// reencodeScalar writes a scalar TLV element from the reader to the writer.
func reencodeScalar(w *Writer, tag Tag, r *Reader) error {
	switch r.Type() {
	case TypeSignedInt8, TypeSignedInt16, TypeSignedInt32, TypeSignedInt64:
		return w.PutSignedInt(tag, r.Value().(int64))
	case TypeUnsignedInt8, TypeUnsignedInt16, TypeUnsignedInt32, TypeUnsignedInt64:
		return w.PutUnsignedInt(tag, r.Value().(uint64))
	case TypeBoolFalse, TypeBoolTrue:
		return w.PutBool(tag, r.Value().(bool))
	case TypeFloat32:
		return w.PutFloat32(tag, r.Value().(float32))
	case TypeFloat64:
		return w.PutFloat64(tag, r.Value().(float64))
	case TypeUTF8String1, TypeUTF8String2, TypeUTF8String4, TypeUTF8String8:
		return w.PutUTF8String(tag, r.Value().(string))
	case TypeOctetString1, TypeOctetString2, TypeOctetString4, TypeOctetString8:
		return w.PutOctetString(tag, r.Value().([]byte))
	case TypeNull:
		return w.PutNull(tag)
	default:
		return fmt.Errorf("rawstruct: unsupported element type %s", r.Type())
	}
}

// captureElement captures the current TLV element (scalar or container) as raw
// TLV bytes, re-encoded with an anonymous tag. For containers, it captures all
// nested elements through to the matching EndOfContainer.
func captureElement(r *Reader) ([]byte, error) {
	w := NewWriter()
	tag := AnonymousTag()

	switch r.Type() {
	case TypeStructure, TypeArray, TypeList:
		switch r.Type() {
		case TypeStructure:
			_ = w.StartStructure(tag)
		case TypeArray:
			_ = w.StartArray(tag)
		case TypeList:
			_ = w.StartList(tag)
		}
		depth := 1
		for depth > 0 {
			if err := r.Next(); err != nil {
				return nil, fmt.Errorf("capturing container element: %w", err)
			}
			if r.Type() == TypeEndOfContainer {
				depth--
				if depth > 0 {
					_ = w.EndContainer()
				}
				continue
			}
			innerTag := r.TagValue()
			switch r.Type() {
			case TypeStructure:
				depth++
				_ = w.StartStructure(innerTag)
			case TypeArray:
				depth++
				_ = w.StartArray(innerTag)
			case TypeList:
				depth++
				_ = w.StartList(innerTag)
			default:
				if err := reencodeScalar(w, innerTag, r); err != nil {
					return nil, err
				}
			}
		}
		_ = w.EndContainer()
	default:
		if err := reencodeScalar(w, tag, r); err != nil {
			return nil, err
		}
	}
	return w.Bytes(), nil
}

// WrapStruct wraps pre-encoded TLV field bytes in an anonymous struct
// container, producing a complete TLV struct suitable for Unmarshal.
func WrapStruct(fields []byte) []byte {
	w := NewWriter()
	_ = w.PutPreEncodedStruct(AnonymousTag(), fields)
	return w.Bytes()
}
