// Package protowire reads and writes protobuf wire-format messages using plain
// Go struct tags — no .proto file, no codegen, no protobuf runtime.
//
// Tag fields with the field number and wire type:
//
//	type Msg struct {
//	    ID   int32  `proto:"1,varint"`
//	    Name string `proto:"2,string"`
//	    Data []byte `proto:"3,bytes"`
//	    Sub  Other  `proto:"4,bytes"` // nested message
//	}
//
//	data, err := protowire.Marshal(Msg{ID: 7, Name: "x"})
//	msg, err := protowire.Unmarshal[Msg](data)
//
// Wire types:
//
//	varint  int32, int64, int, uint32, uint64, uint, bool — plus their slices,
//	        which become packed repeated varints
//	enum    same as varint; nicer to read on enum-style int32 fields
//	string  string, []string
//	bytes   []byte, struct, *struct, []struct, []*struct
//
// Things worth knowing:
//
//   - Zero values are omitted when marshaling (proto3 style): 0, false, "",
//     nil, and empty slices produce no output.
//   - Fields are written in ascending field-number order.
//   - Unknown fields in the input are skipped, so newer data can be decoded by
//     older code.
//   - Unmarshal doesn't copy string or []byte payloads — they point into the
//     input buffer. Don't reuse that buffer until you're done with the result.
//   - Repeated fields append to any values already in the slice.
//
// Errors wrap one of ErrInvalidTag, ErrUnsupportedWireType, ErrBufferUnderflow,
// or ErrVarintOverflow, so errors.Is works.
//
// Not supported: fixed32/fixed64, zigzag-encoded sint fields, oneof.
package protowire
