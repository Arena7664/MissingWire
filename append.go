package missingwire

import (
	"encoding/binary"
	"reflect"
	"unsafe"
)

// appendFieldTag appends fi.tag.
func appendFieldTag(buf []byte, fi *fieldInfo) []byte {
	return appendUvarint(buf, fi.tag)
}

// appendStructTI appends the wire encoding of a struct to buf.
func appendStructTI(buf []byte, ti *typeInfo, ptr unsafe.Pointer) ([]byte, error) {
	for i := range ti.fields {
		fi := &ti.fields[i]
		fptr := unsafe.Add(ptr, fi.offset)

		if fi.elemSize != 0 {
			b, err := appendSliceField(buf, fi, fptr)
			if err != nil {
				return nil, err
			}
			buf = b
			continue
		}

		if isZeroUnsafe(fi, fptr) {
			continue
		}

		b, err := appendScalarField(buf, fi, fptr)
		if err != nil {
			return nil, err
		}
		buf = b
	}
	return buf, nil
}

// appendSliceField appends a slice field to buf.
func appendSliceField(buf []byte, fi *fieldInfo, ptr unsafe.Pointer) ([]byte, error) {
	sh := (*reflect.SliceHeader)(ptr)
	if sh.Len == 0 {
		return buf, nil
	}

	switch fi.wireType {
	case WireVarint:
		// Packed varints go out as a WireBytes field: write the tag, reserve
		// room for the length, append the payload, then backfill the length.
		buf = marshalTag(buf, fi.fieldNum, WireBytes)
		lenOffset, buf := reserveVarintLength(buf)
		start := len(buf)
		for i := 0; i < sh.Len; i++ {
			elemPtr := unsafe.Add(unsafe.Pointer(sh.Data), i*int(fi.elemSize))
			v, _ := varintValUnsafe(fi.kind, elemPtr)
			buf = appendUvarint(buf, v)
		}
		packedSize := len(buf) - start
		var lenArr [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(lenArr[:], uint64(packedSize))
		copy(buf[lenOffset+n:], buf[start:])
		copy(buf[lenOffset:], lenArr[:n])
		buf = buf[:lenOffset+n+packedSize]
		return buf, nil

	case WireBytes:
		if fi.kind == reflect.String {
			for i := 0; i < sh.Len; i++ {
				elemPtr := unsafe.Add(unsafe.Pointer(sh.Data), i*int(fi.elemSize))
				s := *(*string)(elemPtr)
				buf = appendFieldTag(buf, fi)
				buf = appendUvarint(buf, uint64(len(s)))
				buf = append(buf, s...)
			}
			return buf, nil
		}
		if fi.kind == reflect.Struct {
			var err error
			for i := 0; i < sh.Len; i++ {
				elemPtr := unsafe.Add(unsafe.Pointer(sh.Data), i*int(fi.elemSize))
				var structPtr unsafe.Pointer
				if fi.ptrElem {
					structPtr = *(*unsafe.Pointer)(elemPtr)
					if structPtr == nil {
						continue
					}
				} else {
					structPtr = elemPtr
				}
				buf, err = appendLengthDelimitedStruct(buf, fi, fi.elemStructTI, structPtr)
				if err != nil {
					return nil, err
				}
			}
			return buf, nil
		}
		// []byte — singular bytes field
		b := *(*[]byte)(ptr)
		buf = appendFieldTag(buf, fi)
		buf = appendUvarint(buf, uint64(len(b)))
		buf = append(buf, b...)
		return buf, nil
	}
	return buf, nil
}

// appendScalarField appends a scalar field to buf.
func appendScalarField(buf []byte, fi *fieldInfo, ptr unsafe.Pointer) ([]byte, error) {
	switch fi.wireType {
	case WireVarint:
		v, _ := varintValUnsafe(fi.kind, ptr)
		buf = appendFieldTag(buf, fi)
		buf = appendUvarint(buf, v)
		return buf, nil
	case WireBytes:
		if fi.kind == reflect.Struct {
			if fi.ptrElem && fi.elemSize == 0 {
				// *Struct scalar: deref pointer, omit if nil.
				pptr := *(*unsafe.Pointer)(ptr)
				if pptr == nil {
					return buf, nil
				}
				return appendLengthDelimitedStruct(buf, fi, fi.structTI, pptr)
			}
			return appendLengthDelimitedStruct(buf, fi, fi.structTI, ptr)
		}
		s := *(*string)(ptr)
		buf = appendFieldTag(buf, fi)
		buf = appendUvarint(buf, uint64(len(s)))
		buf = append(buf, s...)
		return buf, nil
	}
	return buf, nil
}

// appendLengthDelimitedStruct writes tag | length | fields for a nested message,
// reserving the length slot and backfilling it so no intermediate buffer is needed.
func appendLengthDelimitedStruct(buf []byte, fi *fieldInfo, ti *typeInfo, ptr unsafe.Pointer) ([]byte, error) {
	buf = appendFieldTag(buf, fi)
	lenOffset, buf := reserveVarintLength(buf)
	start := len(buf)
	buf, err := appendStructTI(buf, ti, ptr)
	if err != nil {
		return nil, err
	}
	innerLen := len(buf) - start
	var lenArr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(lenArr[:], uint64(innerLen))
	copy(buf[lenOffset+n:], buf[start:])
	copy(buf[lenOffset:], lenArr[:n])
	return buf[:lenOffset+n+innerLen], nil
}

// appendVarintToSlice appends a single varint value to a slice field.
func appendVarintToSlice(fi *fieldInfo, ptr unsafe.Pointer, val uint64) error {
	dst := growSliceHeader(ptr, fi.elemSize)
	return setVarintUnsafe(fi.kind, dst, val)
}

// appendStringToSlice appends a single string value to a []string field.
func appendStringToSlice(fi *fieldInfo, ptr unsafe.Pointer, s string) {
	dst := growSliceHeader(ptr, fi.elemSize)
	*(*string)(dst) = s
}

// appendUvarint appends a varint-encoded uint64 to buf.
func appendUvarint(buf []byte, x uint64) []byte {
	for x >= 0x80 {
		buf = append(buf, byte(x)|0x80)
		x >>= 7
	}
	return append(buf, byte(x))
}
