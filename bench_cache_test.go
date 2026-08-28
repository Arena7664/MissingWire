package protowire

import (
	"reflect"
	"sync"
	"testing"
)

// Bench types shared by the tests below.

type benchMessage struct {
	ID       int32         `proto:"1,varint"`
	Name     string        `proto:"2,string"`
	Flag     bool          `proto:"3,varint"`
	Data     []byte        `proto:"4,bytes"`
	Inner    benchNested   `proto:"5,bytes"`
	Tags     []string      `proto:"6,string"`
	Counts   []int32       `proto:"7,varint"`
	Children []benchNested `proto:"8,bytes"`
}

type benchNested struct {
	Key   string `proto:"1,string"`
	Value int64  `proto:"2,varint"`
}

// A plain map behind a RWMutex, for comparison.

var (
	rwCache   = make(map[reflect.Type]*typeInfo)
	rwCacheMu sync.RWMutex
)

func getTypeInfoRWMutex(t reflect.Type) (*typeInfo, error) {
	rwCacheMu.RLock()
	info, ok := rwCache[t]
	rwCacheMu.RUnlock()
	if ok {
		return info, nil
	}
	rwCacheMu.Lock()
	if info, ok = rwCache[t]; ok {
		rwCacheMu.Unlock()
		return info, nil
	}
	info, err := buildTypeInfo(t)
	if err != nil {
		rwCacheMu.Unlock()
		return nil, err
	}
	rwCache[t] = info
	rwCacheMu.Unlock()
	return info, nil
}

// The sync.Map version actually used by getTypeInfo.

var smCache sync.Map

func getTypeInfoSyncMap(t reflect.Type) (*typeInfo, error) {
	if v, ok := smCache.Load(t); ok {
		return v.(*typeInfo), nil
	}
	info, err := buildTypeInfo(t)
	if err != nil {
		return nil, err
	}
	actual, loaded := smCache.LoadOrStore(t, info)
	if loaded {
		return actual.(*typeInfo), nil
	}
	return info, nil
}

// Types to exercise the cache with (multiple types for realistic lookup patterns).
var cacheBenchTypes = []reflect.Type{
	reflect.TypeOf(benchMessage{}),
	reflect.TypeOf(benchNested{}),
	reflect.TypeOf(testMixed{}),
	reflect.TypeOf(testVarint{}),
	reflect.TypeOf(testRepeatedStruct{}),
}

func init() {
	// Warm both caches so the benchmarks only measure the lookup path.
	for _, t := range cacheBenchTypes {
		getTypeInfoRWMutex(t)
		getTypeInfoSyncMap(t)
	}
}

// Benchmark the cache lookup only (caches already warmed).
func BenchmarkCacheLookup_RWMutex(b *testing.B) {
	types := cacheBenchTypes
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, t := range types {
			_, err := getTypeInfoRWMutex(t)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkCacheLookup_SyncMap(b *testing.B) {
	types := cacheBenchTypes
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, t := range types {
			_, err := getTypeInfoSyncMap(t)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Full encode/decode benchmarks.

var benchMsg = benchMessage{
	ID:     42,
	Name:   "benchmark-message",
	Flag:   true,
	Data:   []byte{0xDE, 0xAD, 0xBE, 0xEF},
	Inner:  benchNested{Key: "inner", Value: 999},
	Tags:   []string{"alpha", "beta", "gamma"},
	Counts: []int32{1, 2, 3, 4, 5},
	Children: []benchNested{
		{Key: "a", Value: 1},
		{Key: "b", Value: 2},
		{Key: "c", Value: 3},
	},
}

var benchData []byte

func init() {
	var err error
	benchData, err = Marshal(benchMsg)
	if err != nil {
		panic(err)
	}
}

func BenchmarkMarshal_syncMap(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, err := Marshal(benchMsg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalAppend(b *testing.B) {
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		_, err := MarshalAppend(buf[:0], benchMsg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_syncMap(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, err := Unmarshal[benchMessage](benchData)
		if err != nil {
			b.Fatal(err)
		}
	}
}
