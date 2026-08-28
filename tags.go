package protowire

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Wire type constants matching protobuf wire format.
const (
	WireVarint = 0
	WireBytes  = 2
)

// fieldInfo is everything we need to encode/decode one proto-tagged field.
type fieldInfo struct {
	fieldNum     int
	wireType     int
	offset       uintptr      // byte offset of the field in the struct
	kind         reflect.Kind // Go kind (Struct when the field is *Struct or []*Struct)
	elemSize     uintptr      // size of one element; 0 for non-slices
	ptrElem      bool         // true for *Struct and []*Struct
	typ          reflect.Type // field type (element type when ptrElem is set)
	structTI     *typeInfo    // typeInfo for scalar struct fields
	elemStructTI *typeInfo    // typeInfo for []Struct / []*Struct elements
	tag          uint64       // fieldNum<<3 | wireType
	tagSize      int          // encoded size of tag
}

// typeInfo holds all proto-tagged fields for a given struct type, indexed by field number.
type typeInfo struct {
	fields []fieldInfo        // all fields, sorted by field number for marshal
	byNum  map[int]*fieldInfo // lookup by field number for unmarshal
}

var typeCache sync.Map // reflect.Type -> *typeInfo

var (
	ErrInvalidTag          = errors.New("protowire: invalid proto tag")
	ErrUnsupportedWireType = errors.New("protowire: unsupported wire type")
	ErrBufferUnderflow     = errors.New("protowire: buffer underflow")
	ErrVarintOverflow      = errors.New("protowire: varint overflow")
)

// wireTypeName maps tag string to wire type integer.
var wireTypeName = map[string]int{
	"varint": WireVarint,
	"string": WireBytes,
	"bytes":  WireBytes,
	"enum":   WireVarint,
}

// getTypeInfo returns the cached typeInfo for t, building it from struct tags on
// first use.
func getTypeInfo(t reflect.Type) (*typeInfo, error) {
	if v, ok := typeCache.Load(t); ok {
		return v.(*typeInfo), nil
	}

	info, err := buildTypeInfo(t)
	if err != nil {
		return nil, err
	}

	actual, loaded := typeCache.LoadOrStore(t, info)
	if loaded {
		// Another goroutine beat us — use its result
		return actual.(*typeInfo), nil
	}
	return info, nil
}

// buildTypeInfo reflects on t and extracts proto tag data from its fields.
func buildTypeInfo(t reflect.Type) (*typeInfo, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("protowire: Unmarshal target must be a struct, got %v", t.Kind())
	}

	ti := &typeInfo{
		byNum: make(map[int]*fieldInfo),
	}

	for f := range t.Fields() {
		tag := f.Tag.Get("proto")
		if tag == "" {
			continue
		}

		if f.PkgPath != "" {
			return nil, fmt.Errorf("%w: field %q is unexported", ErrInvalidTag, f.Name)
		}

		parts := strings.Split(tag, ",")
		if len(parts) != 2 {
			return nil, fmt.Errorf("%w: field %q tag %q: expected format \"N,wiretype\"", ErrInvalidTag, f.Name, tag)
		}

		fieldNum, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("%w: field %q: invalid field number %q", ErrInvalidTag, f.Name, parts[0])
		}
		if fieldNum <= 0 {
			return nil, fmt.Errorf("%w: field %q: field number must be > 0, got %d", ErrInvalidTag, f.Name, fieldNum)
		}

		wt, ok := wireTypeName[parts[1]]
		if !ok {
			return nil, fmt.Errorf("%w: field %q: unknown wire type %q", ErrUnsupportedWireType, f.Name, parts[1])
		}

		// Validate Go type matches wire type
		if err := validateGoType(f.Name, f.Type, wt); err != nil {
			return nil, err
		}

		tagVal := uint64(fieldNum)<<3 | uint64(wt)
		fi := &fieldInfo{
			fieldNum: fieldNum,
			wireType: wt,
			offset:   f.Offset,
			typ:      f.Type,
			tag:      tagVal,
			tagSize:  sizeOfUvarint(tagVal),
		}
		if f.Type.Kind() == reflect.Slice {
			fi.kind = f.Type.Elem().Kind()
			fi.elemSize = f.Type.Elem().Size()
			// []*Struct: pretend it's a struct and remember to dereference.
			if fi.kind == reflect.Ptr && f.Type.Elem().Elem().Kind() == reflect.Struct {
				fi.kind = reflect.Struct
				fi.ptrElem = true
			}
			if fi.kind == reflect.Struct {
				if fi.ptrElem {
					fi.elemStructTI, _ = getTypeInfo(f.Type.Elem().Elem())
				} else {
					fi.elemStructTI, _ = getTypeInfo(f.Type.Elem())
				}
			}
		} else {
			fi.kind = f.Type.Kind()
			// *Struct: same trick as []*Struct above.
			if fi.kind == reflect.Ptr && f.Type.Elem().Kind() == reflect.Struct {
				fi.kind = reflect.Struct
				fi.ptrElem = true
				fi.typ = f.Type.Elem() // store element type for recursive (un)marshal
			}
		}
		ti.fields = append(ti.fields, *fi)
		if _, exists := ti.byNum[fieldNum]; exists {
			return nil, fmt.Errorf("%w: duplicate field number %d on field %q", ErrInvalidTag, fieldNum, f.Name)
		}
		ti.byNum[fieldNum] = fi
	}

	sort.Slice(ti.fields, func(i, j int) bool {
		return ti.fields[i].fieldNum < ti.fields[j].fieldNum
	})

	// Resolve nested struct metadata up front so encode/decode paths don't have
	// to touch the cache for every field.
	for i := range ti.fields {
		fi := &ti.fields[i]
		if fi.elemSize == 0 && fi.kind == reflect.Struct {
			fi.structTI, _ = getTypeInfo(fi.typ)
		}
	}

	return ti, nil
}

// validateGoType checks that the Go type is compatible with the wire type.
func validateGoType(fieldName string, goType reflect.Type, wireType int) error {
	kind := goType.Kind()
	switch wireType {
	case WireVarint:
		if kind == reflect.Slice {
			elemKind := goType.Elem().Kind()
			switch elemKind {
			case reflect.Int32, reflect.Int64, reflect.Int,
				reflect.Uint32, reflect.Uint64, reflect.Uint,
				reflect.Bool:
				return nil
			}
		}
		switch kind {
		case reflect.Int32, reflect.Int64, reflect.Int,
			reflect.Uint32, reflect.Uint64, reflect.Uint,
			reflect.Bool:
			return nil
		default:
			return fmt.Errorf("%w: field %q: varint wire type requires int32/int64/uint32/uint64/bool, got %v",
				ErrInvalidTag, fieldName, goType)
		}
	case WireBytes:
		switch kind {
		case reflect.String:
			return nil
		case reflect.Struct:
			return nil
		case reflect.Ptr:
			if goType.Elem().Kind() == reflect.Struct {
				return nil // *Struct (pointer to nested message)
			}
			return fmt.Errorf("%w: field %q: pointer-to-struct wire type requires a struct target, got %v",
				ErrInvalidTag, fieldName, goType)
		case reflect.Slice:
			elemKind := goType.Elem().Kind()
			if elemKind == reflect.Uint8 {
				return nil // []byte
			}
			if elemKind == reflect.String {
				return nil // []string
			}
			if elemKind == reflect.Struct {
				return nil // []Struct (repeated message)
			}
			if elemKind == reflect.Ptr && goType.Elem().Elem().Kind() == reflect.Struct {
				return nil // []*Struct (repeated message pointers)
			}
			return fmt.Errorf("%w: field %q: bytes/string wire type requires string, []byte, []string, []struct, *struct, or struct, got %v",
				ErrInvalidTag, fieldName, goType)
		default:
			return fmt.Errorf("%w: field %q: bytes/string wire type requires string, []byte, []string, *struct, or struct, got %v",
				ErrInvalidTag, fieldName, goType)
		}
	default:
		return fmt.Errorf("%w: unknown wire type %d for field %q", ErrUnsupportedWireType, wireType, fieldName)
	}
}
