package protowire

import (
	"testing"
)

type (
	testVarint struct {
		A int32  `proto:"1,varint"`
		B int64  `proto:"2,varint"`
		C uint32 `proto:"3,varint"`
		D bool   `proto:"4,varint"`
	}

	testString struct {
		S string `proto:"1,string"`
	}

	testBytes struct {
		B []byte `proto:"1,bytes"`
	}

	testMixed struct {
		ID   int32  `proto:"1,varint"`
		Name string `proto:"2,string"`
		Data []byte `proto:"3,bytes"`
		Flag bool   `proto:"4,varint"`
	}

	testEnum struct {
		Val int32 `proto:"1,enum"`
	}

	testExtraFields struct {
		A int32 `proto:"1,varint"`
	}

	testRepeatedVarint struct {
		IDs []int32 `proto:"1,varint"`
	}

	testRepeatedString struct {
		Names []string `proto:"1,string"`
	}

	testRepeatedBool struct {
		Flags []bool `proto:"1,varint"`
	}

	testNested struct {
		Name  string `proto:"1,string"`
		Value int32  `proto:"2,varint"`
	}

	testRepeatedStruct struct {
		Items []testNested `proto:"1,bytes"`
	}

	testOuterWithNestedAndRepeated struct {
		ID    int32        `proto:"1,varint"`
		Meta  testNested   `proto:"2,bytes"`
		Items []testNested `proto:"3,bytes"`
	}

	testRepeatedPtrStruct struct {
		Items []*testNested `proto:"1,bytes"`
	}

	testRepeatedPtrStructEmpty struct {
		Items []*testNested `proto:"1,bytes"`
	}
)

func TestRoundTripVarint(t *testing.T) {
	orig := testVarint{A: 42, B: -100, C: 999, D: true}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testVarint](data)
	if err != nil {
		t.Fatal(err)
	}
	if orig != got {
		t.Errorf("round-trip mismatch:\n  want: %+v\n  got:  %+v", orig, got)
	}
}

func TestRoundTripString(t *testing.T) {
	orig := testString{S: "hello world"}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testString](data)
	if err != nil {
		t.Fatal(err)
	}
	if orig != got {
		t.Errorf("round-trip mismatch:\n  want: %+v\n  got:  %+v", orig, got)
	}
}

func TestRoundTripBytes(t *testing.T) {
	orig := testBytes{B: []byte{0xDE, 0xAD, 0xBE, 0xEF}}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testBytes](data)
	if err != nil {
		t.Fatal(err)
	}
	if string(orig.B) != string(got.B) {
		t.Errorf("round-trip mismatch:\n  want: %x\n  got:  %x", orig.B, got.B)
	}
}

func TestRoundTripMixed(t *testing.T) {
	orig := testMixed{
		ID:   10006,
		Name: "Master_Costume_apn_10006",
		Data: []byte{0x01, 0x02, 0x03},
		Flag: true,
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testMixed](data)
	if err != nil {
		t.Fatal(err)
	}
	if orig.ID != got.ID || orig.Name != got.Name || string(orig.Data) != string(got.Data) || orig.Flag != got.Flag {
		t.Errorf("round-trip mismatch:\n  want: %+v\n  got:  %+v", orig, got)
	}
}

func TestRoundTripEnum(t *testing.T) {
	orig := testEnum{Val: 3}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testEnum](data)
	if err != nil {
		t.Fatal(err)
	}
	if orig != got {
		t.Errorf("round-trip mismatch:\n  want: %+v\n  got:  %+v", orig, got)
	}
}

func TestRoundTripNegativeIntegers(t *testing.T) {
	// Negative values have to round-trip, including the int32 minimum.
	orig := testVarint{A: -1, B: -2147483648, C: 0, D: false}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testVarint](data)
	if err != nil {
		t.Fatal(err)
	}
	if orig != got {
		t.Errorf("negative integer round-trip mismatch:\n  want: %+v\n  got:  %+v", orig, got)
	}
}

func TestSkipUnknownFields(t *testing.T) {
	// Marshal a struct with fields 1,2,3,4
	full := testMixed{ID: 1, Name: "test", Data: []byte{0xFF}, Flag: true}
	data, _ := Marshal(full)

	// Unmarshal into a struct that only knows about field 1
	got, err := Unmarshal[testExtraFields](data)
	if err != nil {
		t.Fatal(err)
	}
	if got.A != 1 {
		t.Errorf("expected A=1, got %d", got.A)
	}
}

func TestZeroValueOmission(t *testing.T) {
	orig := testVarint{A: 1, B: 0, C: 0, D: false}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testVarint](data)
	if err != nil {
		t.Fatal(err)
	}
	// B, C, D should all be zero since they were omitted
	if got.A != 1 {
		t.Errorf("expected A=1, got %d", got.A)
	}
	if got.B != 0 {
		t.Errorf("expected B=0 (omitted), got %d", got.B)
	}
	if got.C != 0 {
		t.Errorf("expected C=0 (omitted), got %d", got.C)
	}
	if got.D != false {
		t.Errorf("expected D=false (omitted), got %v", got.D)
	}
}

func TestEmptyInput(t *testing.T) {
	got, err := Unmarshal[testVarint]([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if got != (testVarint{}) {
		t.Errorf("expected zero value, got %+v", got)
	}
}

func TestBufferUnderflow(t *testing.T) {
	// Truncated varint tag (0x80 means "more bytes follow")
	_, err := Unmarshal[testVarint]([]byte{0x80})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRoundTripRepeatedVarint(t *testing.T) {
	orig := testRepeatedVarint{IDs: []int32{1, 2, 3, -5, 100}}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedVarint](data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.IDs) != len(orig.IDs) {
		t.Fatalf("len mismatch: want %d, got %d", len(orig.IDs), len(got.IDs))
	}
	for i := range orig.IDs {
		if got.IDs[i] != orig.IDs[i] {
			t.Errorf("index %d: want %d, got %d", i, orig.IDs[i], got.IDs[i])
		}
	}
}

func TestRoundTripRepeatedString(t *testing.T) {
	orig := testRepeatedString{Names: []string{"alice", "bob", "charlie"}}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedString](data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Names) != len(orig.Names) {
		t.Fatalf("len mismatch: want %d, got %d", len(orig.Names), len(got.Names))
	}
	for i := range orig.Names {
		if got.Names[i] != orig.Names[i] {
			t.Errorf("index %d: want %q, got %q", i, orig.Names[i], got.Names[i])
		}
	}
}

func TestRoundTripRepeatedBool(t *testing.T) {
	orig := testRepeatedBool{Flags: []bool{true, false, true, true}}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedBool](data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Flags) != len(orig.Flags) {
		t.Fatalf("len mismatch: want %d, got %d", len(orig.Flags), len(got.Flags))
	}
	for i := range orig.Flags {
		if got.Flags[i] != orig.Flags[i] {
			t.Errorf("index %d: want %v, got %v", i, orig.Flags[i], got.Flags[i])
		}
	}
}

func TestEmptySliceOmitted(t *testing.T) {
	orig := testRepeatedVarint{IDs: []int32{}}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedVarint](data)
	if err != nil {
		t.Fatal(err)
	}
	if got.IDs != nil {
		t.Errorf("expected nil slice for empty input, got %v", got.IDs)
	}
}

func TestPackedRepeatedDecoding(t *testing.T) {
	// Manually construct a packed repeated varint: tag(1<<3|2=0x0A) len(3) varint(1) varint(2) varint(3)
	data := []byte{0x0A, 0x03, 0x01, 0x02, 0x03}
	got, err := Unmarshal[testRepeatedVarint](data)
	if err != nil {
		t.Fatal(err)
	}
	expected := []int32{1, 2, 3}
	if len(got.IDs) != len(expected) {
		t.Fatalf("len mismatch: want %d, got %d", len(expected), len(got.IDs))
	}
	for i := range expected {
		if got.IDs[i] != expected[i] {
			t.Errorf("index %d: want %d, got %d", i, expected[i], got.IDs[i])
		}
	}
}

func TestRoundTripRepeatedStruct(t *testing.T) {
	orig := testRepeatedStruct{
		Items: []testNested{
			{Name: "first", Value: 1},
			{Name: "second", Value: 2},
			{Name: "third", Value: 3},
		},
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedStruct](data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != len(orig.Items) {
		t.Fatalf("len mismatch: want %d, got %d", len(orig.Items), len(got.Items))
	}
	for i := range orig.Items {
		if got.Items[i] != orig.Items[i] {
			t.Errorf("index %d: want %+v, got %+v", i, orig.Items[i], got.Items[i])
		}
	}
}

func TestRoundTripOuterWithNestedAndRepeated(t *testing.T) {
	orig := testOuterWithNestedAndRepeated{
		ID:   42,
		Meta: testNested{Name: "meta", Value: 99},
		Items: []testNested{
			{Name: "a", Value: 1},
			{Name: "b", Value: 2},
		},
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testOuterWithNestedAndRepeated](data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != orig.ID {
		t.Errorf("ID: want %d, got %d", orig.ID, got.ID)
	}
	if got.Meta != orig.Meta {
		t.Errorf("Meta: want %+v, got %+v", orig.Meta, got.Meta)
	}
	if len(got.Items) != len(orig.Items) {
		t.Fatalf("Items len: want %d, got %d", len(orig.Items), len(got.Items))
	}
	for i := range orig.Items {
		if got.Items[i] != orig.Items[i] {
			t.Errorf("Items[%d]: want %+v, got %+v", i, orig.Items[i], got.Items[i])
		}
	}
}

func TestRoundTripRepeatedPtrStruct(t *testing.T) {
	orig := testRepeatedPtrStruct{
		Items: []*testNested{
			{Name: "first", Value: 1},
			{Name: "second", Value: 2},
			{Name: "fourth", Value: 4},
		},
	}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedPtrStruct](data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != len(orig.Items) {
		t.Fatalf("len mismatch: want %d, got %d", len(orig.Items), len(got.Items))
	}
	for i := range orig.Items {
		if got.Items[i] == nil {
			t.Errorf("index %d: want %+v, got nil", i, orig.Items[i])
			continue
		}
		if *got.Items[i] != *orig.Items[i] {
			t.Errorf("index %d: want %+v, got %+v", i, *orig.Items[i], *got.Items[i])
		}
	}
}

func TestRepeatedPtrStructEmptySliceOmitted(t *testing.T) {
	orig := testRepeatedPtrStructEmpty{}
	data, err := Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal[testRepeatedPtrStructEmpty](data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Items != nil {
		t.Errorf("expected nil slice for empty input, got %v", got.Items)
	}
}
