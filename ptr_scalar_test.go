package protowire

import (
	"testing"
)

type innerMsg struct {
	X int32 `proto:"1,varint"`
}

type outerWithPtrScalar struct {
	Meta *innerMsg `proto:"1,bytes"`
}

type outerWithValueScalar struct {
	Meta innerMsg `proto:"1,bytes"`
}

func TestRoundTripPtrScalarStruct(t *testing.T) {
	orig := outerWithPtrScalar{
		Meta: &innerMsg{X: 42},
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	got, err := Unmarshal[outerWithPtrScalar](data)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Meta == nil {
		t.Fatal("expected non-nil Meta pointer")
	}
	if got.Meta.X != 42 {
		t.Errorf("expected X=42, got %d", got.Meta.X)
	}

	// Verify it round-trips to same wire format as value struct
	dataValue, _ := Marshal(outerWithValueScalar{Meta: innerMsg{X: 42}})
	if string(data) != string(dataValue) {
		t.Errorf("ptr scalar wire format differs from value scalar:\n  ptr: %x\n  val: %x", data, dataValue)
	}
}

func TestPtrScalarNilOmitted(t *testing.T) {
	// Nil pointer should marshal as empty (zero-value omitted)
	orig := outerWithPtrScalar{Meta: nil}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty output for nil *Struct, got %x", data)
	}

	// Unmarshalling empty input should produce nil pointer
	got, err := Unmarshal[outerWithPtrScalar]([]byte{})
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Meta != nil {
		t.Errorf("expected nil Meta after empty unmarshal, got %+v", got.Meta)
	}
}

type outerWithBothPtrForms struct {
	Scalar   *innerMsg   `proto:"1,bytes"`
	Repeated []*innerMsg `proto:"2,bytes"`
}

func TestRoundTripMixedPtrForms(t *testing.T) {
	orig := outerWithBothPtrForms{
		Scalar: &innerMsg{X: 10},
		Repeated: []*innerMsg{
			{X: 1}, {X: 2}, {X: 3},
		},
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	got, err := Unmarshal[outerWithBothPtrForms](data)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if got.Scalar == nil || got.Scalar.X != 10 {
		t.Errorf("scalar ptr mismatch: %+v", got.Scalar)
	}
	if len(got.Repeated) != 3 {
		t.Fatalf("repeated ptr len: got %d, want 3", len(got.Repeated))
	}
	for i, v := range []int32{1, 2, 3} {
		if got.Repeated[i] == nil || got.Repeated[i].X != v {
			t.Errorf("repeated ptr[%d]: got %+v, want X=%d", i, got.Repeated[i], v)
		}
	}
}
