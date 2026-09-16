// Copyright 2026 matter-cli contributors
// SPDX-License-Identifier: Apache-2.0

package tlv

import (
	"bytes"
	"testing"
)

type optionalTestStruct struct {
	Name  string           `tlv:"0,utf8"`
	Count Optional[uint16] `tlv:"1,uint"`
}

type pointerTestStruct struct {
	Name  string  `tlv:"0,utf8"`
	Count *uint16 `tlv:"1,uint"`
}

func TestOptional_ZeroValueIsAbsent(t *testing.T) {
	var o Optional[uint16]
	if !o.IsAbsent() {
		t.Error("zero value should be absent")
	}
	if o.IsNull() {
		t.Error("zero value should not be null")
	}
	if o.IsPresent() {
		t.Error("zero value should not be present")
	}
	if v, ok := o.Get(); ok || v != 0 {
		t.Errorf("Get() = %v, %v, want 0, false", v, ok)
	}
}

func TestOptionalValue(t *testing.T) {
	o := OptionalValue(uint16(42))
	if o.IsAbsent() || o.IsNull() || !o.IsPresent() {
		t.Errorf("OptionalValue(42) state = %+v, want present", o)
	}
	v, ok := o.Get()
	if !ok || v != 42 {
		t.Errorf("Get() = %v, %v, want 42, true", v, ok)
	}
}

func TestOptionalNull(t *testing.T) {
	o := OptionalNull[uint16]()
	if o.IsAbsent() || !o.IsNull() || o.IsPresent() {
		t.Errorf("OptionalNull() state = %+v, want null", o)
	}
	if v, ok := o.Get(); ok || v != 0 {
		t.Errorf("Get() = %v, %v, want 0, false", v, ok)
	}
}

func TestOptional_TLVRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		in    optionalTestStruct
		check func(t *testing.T, decoded optionalTestStruct)
	}{
		{
			name: "absent",
			in:   optionalTestStruct{Name: "a"},
			check: func(t *testing.T, decoded optionalTestStruct) {
				if !decoded.Count.IsAbsent() {
					t.Errorf("Count = %+v, want absent", decoded.Count)
				}
			},
		},
		{
			name: "null",
			in:   optionalTestStruct{Name: "b", Count: OptionalNull[uint16]()},
			check: func(t *testing.T, decoded optionalTestStruct) {
				if !decoded.Count.IsNull() {
					t.Errorf("Count = %+v, want null", decoded.Count)
				}
			},
		},
		{
			name: "value",
			in:   optionalTestStruct{Name: "c", Count: OptionalValue(uint16(7))},
			check: func(t *testing.T, decoded optionalTestStruct) {
				v, ok := decoded.Count.Get()
				if !ok || v != 7 {
					t.Errorf("Count.Get() = %v, %v, want 7, true", v, ok)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Marshal(tt.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var decoded optionalTestStruct
			if err := Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if decoded.Name != tt.in.Name {
				t.Errorf("Name = %q, want %q", decoded.Name, tt.in.Name)
			}
			tt.check(t, decoded)
		})
	}
}

// TestOptional_AbsentMatchesOmittedPointer confirms that an absent
// Optional[T] field produces byte-identical output to the same field
// declared as a plain nil pointer -- the codec's existing "omit when not
// present" behavior, unchanged by this type's introduction.
func TestOptional_AbsentMatchesOmittedPointer(t *testing.T) {
	optData, err := Marshal(optionalTestStruct{Name: "x"})
	if err != nil {
		t.Fatalf("Marshal(optional): %v", err)
	}
	ptrData, err := Marshal(pointerTestStruct{Name: "x"})
	if err != nil {
		t.Fatalf("Marshal(pointer): %v", err)
	}
	if !bytes.Equal(optData, ptrData) {
		t.Errorf("Marshal(optional, absent) = %x, want %x (same as omitted pointer)", optData, ptrData)
	}
}
