package missingwire

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"unsafe"
)

// Unmarshal decodes wire-format protobuf bytes into a new value of type T.
// Fields are matched by their proto:"N,wiretype" tags; unknown field numbers in
// the input are skipped. See the package doc for the supported types.
func Unmarshal[T any](data []byte) (v T, err error) {
	rv := reflect.ValueOf(&v).Elem()
	if err = unmarshalInto(rv.Type(), unsafe.Pointer(&v), data); err != nil {
		return v, err
	}
	return v, nil
}

// unmarshalInto parses wire-format protobuf bytes directly into memory at ptr for type typ.
func unmarshalInto(typ reflect.Type, ptr unsafe.Pointer, data []byte) error {
	ti, err := getTypeInfo(typ)
	if err != nil {
		return err
	}

	pos := 0
	for pos < len(data) {
		tag, n, err := readUvarint(data, pos)
		if err != nil {
			return fmt.Errorf("%w: cannot read tag at offset %d", ErrBufferUnderflow, pos)
		}
		pos += n

		fieldNum := int(tag >> 3)
		wireType := int(tag & 0x7)

		fi := ti.byNum[fieldNum]
		if fi == nil {
			// Unknown field — skip it
			if err := skipField(data, &pos, wireType); err != nil {
				return fmt.Errorf("skip unknown field %d: %w", fieldNum, err)
			}
			continue
		}

		fptr := unsafe.Add(ptr, fi.offset)

		switch wireType {
		case WireVarint:
			val, n, err := readUvarint(data, pos)
			if err != nil {
				return fmt.Errorf("%w: cannot read varint for field %d", ErrBufferUnderflow, fieldNum)
			}
			pos += n

			if fi.elemSize != 0 {
				// Non-packed repeated varint
				if err := appendVarintToSlice(fi, fptr, val); err != nil {
					return fmt.Errorf("field %d: %w", fieldNum, err)
				}
			} else {
				if err := setVarintUnsafe(fi.kind, fptr, val); err != nil {
					return fmt.Errorf("field %d: %w", fieldNum, err)
				}
			}

		case WireBytes:
			// Packed repeated varint
			if fi.elemSize != 0 && fi.wireType == WireVarint {
				length, n, err := readUvarint(data, pos)
				if err != nil {
					return fmt.Errorf("%w: cannot read packed length for field %d", ErrBufferUnderflow, fieldNum)
				}
				pos += n
				if length > math.MaxInt {
					return fmt.Errorf("%w: packed length %d overflows int", ErrBufferUnderflow, length)
				}
				if int(length) > len(data)-pos {
					return fmt.Errorf("%w: field %d: need %d bytes, have %d", ErrBufferUnderflow, fieldNum, length, len(data)-pos)
				}
				blob := data[pos : pos+int(length)]
				pos += int(length)
				if err = decodePackedVarintsUnsafe(fi, fptr, blob); err != nil {
					return fmt.Errorf("field %d: %w", fieldNum, err)
				}
				continue
			}

			length, n, err := readUvarint(data, pos)
			if err != nil {
				return fmt.Errorf("%w: cannot read length for field %d", ErrBufferUnderflow, fieldNum)
			}
			pos += n
			if length > math.MaxInt {
				return fmt.Errorf("%w: length %d overflows int", ErrBufferUnderflow, length)
			}
			if int(length) > len(data)-pos {
				return fmt.Errorf("%w: field %d: need %d bytes, have %d", ErrBufferUnderflow, fieldNum, length, len(data)-pos)
			}
			valBytes := data[pos : pos+int(length)]
			pos += int(length)

			if fi.elemSize != 0 {
				// Slice field in WireBytes path
				switch fi.kind {
				case reflect.String:
					appendStringToSlice(fi, fptr, bytesToString(valBytes))
				case reflect.Struct:
					// Repeated message — unmarshal into new element
					if fi.ptrElem {
						// []*Struct: allocate pointer slot, then allocate and unmarshal struct
						ptrSlot := growSliceHeader(fptr, fi.elemSize)
						elemType := fi.typ.Elem().Elem() // Struct type
						newVal := reflect.New(elemType)
						*(*unsafe.Pointer)(ptrSlot) = unsafe.Pointer(newVal.Pointer())
						if err := unmarshalInto(elemType, unsafe.Pointer(newVal.Pointer()), valBytes); err != nil {
							return fmt.Errorf("field %d: %w", fieldNum, err)
						}
					} else {
						dst := growSliceHeader(fptr, fi.elemSize)
						if err := unmarshalInto(fi.typ.Elem(), dst, valBytes); err != nil {
							return fmt.Errorf("field %d: %w", fieldNum, err)
						}
					}
				default:
					// []byte — singular bytes, alias input buffer
					*(*[]byte)(fptr) = valBytes
				}
			} else {
				if err := setBytesUnsafe(fi, fptr, valBytes); err != nil {
					return fmt.Errorf("field %d: %w", fieldNum, err)
				}
			}

		default:
			return fmt.Errorf("%w: wire type %d for field %d", ErrUnsupportedWireType, wireType, fieldNum)
		}
	}

	return nil
}

// readUvarint decodes a varint from data starting at pos. It returns the value,
// the number of bytes consumed, and an error if the varint is truncated.
func readUvarint(data []byte, pos int) (uint64, int, error) {
	if pos >= len(data) {
		return 0, 0, ErrBufferUnderflow
	}
	if data[pos] < 0x80 {
		return uint64(data[pos]), 1, nil
	}
	var x uint64
	var s uint
	for i := pos; i < len(data); i++ {
		b := data[i]
		x |= uint64(b&0x7f) << s
		if b&0x80 == 0 {
			return x, i - pos + 1, nil
		}
		s += 7
		if s >= 64 {
			return 0, 0, fmt.Errorf("%w: varint overflows 64 bits", ErrVarintOverflow)
		}
	}
	return 0, 0, ErrBufferUnderflow
}

// setVarintUnsafe writes a decoded varint value to a struct field via unsafe pointer.
func setVarintUnsafe(kind reflect.Kind, ptr unsafe.Pointer, val uint64) error {
	switch kind {
	case reflect.Bool:
		*(*bool)(ptr) = val != 0
	case reflect.Int32:
		i := int64(val)
		if i < math.MinInt32 || i > math.MaxInt32 {
			return fmt.Errorf("%w: value %d exceeds int32 range", ErrVarintOverflow, i)
		}
		*(*int32)(ptr) = int32(i)
	case reflect.Int:
		*(*int)(ptr) = int(val)
	case reflect.Int64:
		*(*int64)(ptr) = int64(val)
	case reflect.Uint32:
		if val > math.MaxUint32 {
			return fmt.Errorf("%w: value %d exceeds uint32 range", ErrVarintOverflow, val)
		}
		*(*uint32)(ptr) = uint32(val)
	case reflect.Uint:
		*(*uint)(ptr) = uint(val)
	case reflect.Uint64:
		*(*uint64)(ptr) = val
	default:
		return fmt.Errorf("unsupported Go kind %v for varint", kind)
	}
	return nil
}

// setBytesUnsafe assigns decoded length-delimited bytes to a struct field via unsafe pointer.
func setBytesUnsafe(fi *fieldInfo, ptr unsafe.Pointer, data []byte) error {
	switch fi.kind {
	case reflect.String:
		*(*string)(ptr) = bytesToString(data)
	case reflect.Struct:
		if fi.ptrElem && fi.elemSize == 0 {
			// *Struct scalar: deref pointer, allocate on nil.
			pptr := (*unsafe.Pointer)(ptr)
			if *pptr == nil {
				newVal := reflect.New(fi.typ)
				*pptr = unsafe.Pointer(newVal.Pointer())
			}
			return unmarshalInto(fi.typ, *pptr, data)
		}
		// Recursively unmarshal nested message
		return unmarshalInto(fi.typ, ptr, data)
	default:
		return fmt.Errorf("unsupported Go kind %v for bytes", fi.kind)
	}
	return nil
}

// bytesToString returns a string that aliases b without copying.
// The caller must not mutate b while the string is in use.
func bytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// MarshalAppend encodes v into an existing buffer and returns the result.
// Passing a nil buf is equivalent to calling Marshal.
func MarshalAppend[T any](buf []byte, v T) ([]byte, error) {
	rv := reflect.ValueOf(v)
	ti, err := getTypeInfo(rv.Type())
	if err != nil {
		return buf, err
	}

	base := unsafe.Pointer(&v)
	size, err := sizeOfStructTI(ti, base)
	if err != nil {
		return buf, err
	}

	if cap(buf)-len(buf) < size {
		// Allocate a little extra headroom so length-prefix reservations during
		// encoding don't force the buffer to grow.
		newBuf := make([]byte, len(buf), len(buf)+size+64)
		copy(newBuf, buf)
		buf = newBuf
	}

	return appendStructTI(buf[:], ti, base)
}

// Marshal encodes a struct to wire-format protobuf bytes.
// Only fields with proto tags are encoded. Zero-value fields are omitted (proto3 behavior).
func Marshal[T any](v T) ([]byte, error) {
	return MarshalAppend(nil, v)
}

// marshalTag appends a field tag for fieldNum and wireType.
func marshalTag(buf []byte, fieldNum int, wireType int) []byte {
	return appendUvarint(buf, uint64(fieldNum)<<3|uint64(wireType))
}

// reserveVarintLength reserves up to binary.MaxVarintLen64 bytes in buf for a
// length-prefix varint and returns the offset where the length will be written.
// It tries to grow buf in place; only if capacity is exhausted does it allocate.
func reserveVarintLength(buf []byte) (int, []byte) {
	if cap(buf)-len(buf) < binary.MaxVarintLen64 {
		buf = append(buf, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	} else {
		buf = buf[:len(buf)+binary.MaxVarintLen64]
	}
	return len(buf) - binary.MaxVarintLen64, buf
}

// growSliceHeader grows the slice at ptr by one element and returns a pointer to the new element.
func growSliceHeader(ptr unsafe.Pointer, elemSize uintptr) unsafe.Pointer {
	sh := (*reflect.SliceHeader)(ptr)
	newLen := sh.Len + 1
	if newLen <= sh.Cap {
		// Reuse existing backing array
		sh.Len = newLen
	} else {
		// Allocate new backing array
		newCap := max(newLen, 2*sh.Cap)
		newData := make([]byte, newCap*int(elemSize))
		if sh.Len > 0 {
			copy(newData, unsafe.Slice((*byte)(unsafe.Pointer(sh.Data)), sh.Len*int(elemSize)))
		}
		sh.Data = uintptr(unsafe.Pointer(unsafe.SliceData(newData)))
		sh.Len = newLen
		sh.Cap = newCap
	}
	return unsafe.Add(unsafe.Pointer(sh.Data), uintptr(sh.Len-1)*elemSize)
}

// decodePackedVarintsUnsafe reads varints from a packed repeated blob into a slice field via unsafe.
// Allocates a backing array sized exactly to the number of varints in the blob.
func decodePackedVarintsUnsafe(fi *fieldInfo, ptr unsafe.Pointer, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	count, err := countPackedVarints(data)
	if err != nil {
		return err
	}

	elemSize := int(fi.elemSize)
	backing := make([]byte, count*elemSize)

	p := 0
	idx := 0
	for p < len(data) {
		val, n, err := readUvarint(data, p)
		if err != nil {
			return err
		}
		p += n
		elemPtr := unsafe.Add(unsafe.Pointer(unsafe.SliceData(backing)), idx*elemSize)
		if err := setVarintUnsafe(fi.kind, elemPtr, val); err != nil {
			return err
		}
		idx++
	}

	// Write slice header to the struct field
	sh := (*reflect.SliceHeader)(ptr)
	sh.Data = uintptr(unsafe.Pointer(unsafe.SliceData(backing)))
	sh.Len = count
	sh.Cap = count
	return nil
}

// countPackedVarints returns the number of varints in a packed repeated varint blob.
func countPackedVarints(data []byte) (int, error) {
	count := 0
	p := 0
	for p < len(data) {
		_, n, err := readUvarint(data, p)
		if err != nil {
			return 0, err
		}
		p += n
		count++
	}
	return count, nil
}

// skipField advances pos past an unknown field's value.
func skipField(data []byte, pos *int, wireType int) error {
	switch wireType {
	case WireVarint:
		_, n, err := readUvarint(data, *pos)
		if err != nil {
			return fmt.Errorf("%w: cannot skip varint", ErrBufferUnderflow)
		}
		*pos += n
	case WireBytes:
		length, n, err := readUvarint(data, *pos)
		if err != nil {
			return fmt.Errorf("%w: cannot skip LEN length", ErrBufferUnderflow)
		}
		if length > math.MaxInt {
			return fmt.Errorf("%w: length %d overflows int", ErrBufferUnderflow, length)
		}
		*pos += n + int(length)
		if *pos > len(data) {
			return ErrBufferUnderflow
		}
	default:
		return fmt.Errorf("cannot skip wire type %d", wireType)
	}
	return nil
}

// varintValUnsafe reads a varint-compatible value from an unsafe pointer.
func varintValUnsafe(kind reflect.Kind, ptr unsafe.Pointer) (uint64, error) {
	switch kind {
	case reflect.Bool:
		if *(*bool)(ptr) {
			return 1, nil
		}
		return 0, nil
	case reflect.Int32:
		return uint64(*(*int32)(ptr)), nil
	case reflect.Int:
		return uint64(*(*int)(ptr)), nil
	case reflect.Int64:
		return uint64(*(*int64)(ptr)), nil
	case reflect.Uint32:
		return uint64(*(*uint32)(ptr)), nil
	case reflect.Uint:
		return uint64(*(*uint)(ptr)), nil
	case reflect.Uint64:
		return *(*uint64)(ptr), nil
	default:
		return 0, fmt.Errorf("unsupported Go kind %v for varint encoding", kind)
	}
}

// isZeroUnsafe checks whether a field value at ptr is the zero value for its kind.
func isZeroUnsafe(fi *fieldInfo, ptr unsafe.Pointer) bool {
	switch fi.kind {
	case reflect.Bool:
		return !*(*bool)(ptr)
	case reflect.Int32:
		return *(*int32)(ptr) == 0
	case reflect.Int64:
		return *(*int64)(ptr) == 0
	case reflect.Int:
		return *(*int)(ptr) == 0
	case reflect.Uint32:
		return *(*uint32)(ptr) == 0
	case reflect.Uint64:
		return *(*uint64)(ptr) == 0
	case reflect.Uint:
		return *(*uint)(ptr) == 0
	case reflect.String:
		return *(*string)(ptr) == ""
	case reflect.Struct:
		if fi.ptrElem && fi.elemSize == 0 {
			// *Struct scalar: nil pointer == zero value.
			return *(*unsafe.Pointer)(ptr) == nil
		}
		// A non-pointer nested struct always gets encoded, even when empty.
		return false
	}
	return false
}
