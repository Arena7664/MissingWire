package missingwire

import (
	"reflect"
	"unsafe"
)

// sizeOfUvarint returns the encoded length of x in protobuf varint format.
func sizeOfUvarint(x uint64) int {
	size := 1
	for x >= 0x80 {
		size++
		x >>= 7
	}
	return size
}

// sizeOfStructTI returns the encoded size of a struct value.
func sizeOfStructTI(ti *typeInfo, ptr unsafe.Pointer) (int, error) {
	size := 0
	for i := range ti.fields {
		fi := &ti.fields[i]
		fptr := unsafe.Add(ptr, fi.offset)

		if fi.elemSize != 0 {
			s, err := sizeOfSliceField(fi, fptr)
			if err != nil {
				return 0, err
			}
			size += s
			continue
		}

		if isZeroUnsafe(fi, fptr) {
			continue
		}

		s, err := sizeOfScalarField(fi, fptr)
		if err != nil {
			return 0, err
		}
		size += s
	}
	return size, nil
}

// sizeOfSliceField returns the encoded size of a slice field.
func sizeOfSliceField(fi *fieldInfo, ptr unsafe.Pointer) (int, error) {
	sh := (*reflect.SliceHeader)(ptr)
	if sh.Len == 0 {
		return 0, nil
	}

	switch fi.wireType {
	case WireVarint:
		// Packed repeated varints are emitted as a WireBytes field.
		packed := 0
		for i := 0; i < sh.Len; i++ {
			elemPtr := unsafe.Add(unsafe.Pointer(sh.Data), i*int(fi.elemSize))
			v, _ := varintValUnsafe(fi.kind, elemPtr)
			packed += sizeOfUvarint(v)
		}
		return sizeOfUvarint(uint64(fi.fieldNum)<<3|uint64(WireBytes)) +
			sizeOfUvarint(uint64(packed)) + packed, nil

	case WireBytes:
		tagSize := fi.tagSize
		if fi.kind == reflect.String {
			size := 0
			for i := 0; i < sh.Len; i++ {
				elemPtr := unsafe.Add(unsafe.Pointer(sh.Data), i*int(fi.elemSize))
				s := *(*string)(elemPtr)
				size += tagSize + sizeOfUvarint(uint64(len(s))) + len(s)
			}
			return size, nil
		}
		if fi.kind == reflect.Struct {
			size := 0
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
				innerSize, err := sizeOfStructTI(fi.elemStructTI, structPtr)
				if err != nil {
					return 0, err
				}
				size += tagSize + sizeOfUvarint(uint64(innerSize)) + innerSize
			}
			return size, nil
		}
		// []byte — singular bytes field
		b := *(*[]byte)(ptr)
		return tagSize + sizeOfUvarint(uint64(len(b))) + len(b), nil
	}
	return 0, nil
}

// sizeOfScalarField returns the encoded size of a non-slice field.
func sizeOfScalarField(fi *fieldInfo, ptr unsafe.Pointer) (int, error) {
	switch fi.wireType {
	case WireVarint:
		v, _ := varintValUnsafe(fi.kind, ptr)
		return fi.tagSize + sizeOfUvarint(v), nil
	case WireBytes:
		tagSize := fi.tagSize
		if fi.kind == reflect.Struct {
			innerSize, err := sizeOfStructTI(fi.structTI, ptr)
			if err != nil {
				return 0, err
			}
			return tagSize + sizeOfUvarint(uint64(innerSize)) + innerSize, nil
		}
		s := *(*string)(ptr)
		return tagSize + sizeOfUvarint(uint64(len(s))) + len(s), nil
	}
	return 0, nil
}
