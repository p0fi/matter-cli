// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import "reflect"

// optionalState distinguishes the three states an Optional[T] can be in.
type optionalState uint8

const (
	// optionalAbsent means the field is not present on the wire at all.
	// This is the zero value of optionalState, so the zero value of
	// Optional[T] is absent.
	optionalAbsent optionalState = iota
	// optionalNull means the field is present on the wire as an explicit
	// TLV null.
	optionalNull
	// optionalValuePresent means the field carries a concrete value.
	optionalValuePresent
)

// Optional is a tri-state wrapper for struct fields that must distinguish a
// value that is absent from the wire entirely, from one explicitly encoded
// as TLV null, from one carrying a concrete value. The zero value is the
// absent state, so declaring an Optional[T] field without assigning it
// encodes as omitted rather than as a spurious null.
//
// Use OptionalValue to build a value-present instance and OptionalNull to
// build an explicit-null instance. Use IsAbsent, IsNull, IsPresent, and Get
// to inspect one.
type Optional[T any] struct {
	state optionalState
	value T
}

// OptionalValue returns an Optional[T] carrying v.
func OptionalValue[T any](v T) Optional[T] {
	return Optional[T]{state: optionalValuePresent, value: v}
}

// OptionalNull returns an Optional[T] in the explicit-null state.
func OptionalNull[T any]() Optional[T] {
	return Optional[T]{state: optionalNull}
}

// IsAbsent reports whether the value is absent from the wire (or was never
// assigned).
func (o Optional[T]) IsAbsent() bool {
	return o.state == optionalAbsent
}

// IsNull reports whether the value is an explicit TLV null.
func (o Optional[T]) IsNull() bool {
	return o.state == optionalNull
}

// IsPresent reports whether the value carries a concrete T.
func (o Optional[T]) IsPresent() bool {
	return o.state == optionalValuePresent
}

// Get returns the wrapped value and whether it is present. When not
// present, the returned value is the zero value of T.
func (o Optional[T]) Get() (T, bool) {
	return o.value, o.state == optionalValuePresent
}

// optionalState reports which of the three states the field is in. It
// backs the optionalReader interface that marshal.go's reflection-based
// Marshal uses to inspect a tri-state field without a type parameter of its
// own.
func (o Optional[T]) optionalState() optionalState {
	return o.state
}

// optionalValue returns the wrapped T as an any, valid when optionalState
// reports optionalValuePresent. It backs the optionalReader interface.
func (o Optional[T]) optionalValue() any {
	return o.value
}

// setOptionalNull puts the field into the explicit-null state. It backs
// the optionalWriter interface that marshal.go's reflection-based
// Unmarshal uses to mutate a tri-state field in place without a type
// parameter of its own.
func (o *Optional[T]) setOptionalNull() {
	o.state = optionalNull
	var zero T
	o.value = zero
}

// optionalDecodeTarget marks the field as value-present and returns an
// addressable, settable reflect.Value for the wrapped T to decode into. It
// backs the optionalWriter interface.
func (o *Optional[T]) optionalDecodeTarget() reflect.Value {
	o.state = optionalValuePresent
	return reflect.ValueOf(&o.value).Elem()
}
