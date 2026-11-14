package hashmap_test

import (
	"errors"
	"maps"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/internal/testkit"
	"github.com/stubbedev/memory-go/memtest"
)

type key struct {
	A uint16
	B [2]int8
}

func TestRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := hashmap.New[string, int](a, 0); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Errorf("string key: err = %v, want ErrHoldsPointers", err)
	}
	if _, err := hashmap.New[int, []byte](a, 0); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Errorf("slice value: err = %v, want ErrHoldsPointers", err)
	}
	if _, err := hashmap.New[int, int](a, -1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Errorf("capacity -1: err = %v, want ErrNegativeLen", err)
	}
}

func TestCapacityAvoidsGrowth(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	for _, capacity := range []int{0, 1, 6, 7, 12, 13, 1000} {
		m, err := hashmap.New[int, int](a, capacity)
		if err != nil {
			t.Fatal(err)
		}
		before := a.Stats()
		for k := range capacity {
			if err := m.Set(k, k); err != nil {
				t.Fatal(err)
			}
		}
		if after := a.Stats(); after != before {
			t.Errorf("capacity %d: allocator stats %+v -> %+v, want no growth", capacity, before, after)
		}
		if err := m.Free(); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzMatchesMap(f *testing.F) {
	f.Add(testkit.Seed(3, 1<<16))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := hashmap.New[key, uint64](memtest.Local(t), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer m.Free()
		matchesMap(t, m, data)
	})
}

func TestMatchesMapOnGuarded(t *testing.T) {
	t.Parallel()

	m, err := hashmap.New[key, uint64](memtest.Guarded(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	matchesMap(t, m, testkit.Seed(3, 1<<16))
}

func matchesMap(t *testing.T, m *hashmap.Map[key, uint64], data []byte) {
	t.Helper()
	ops := testkit.NewOps(data)
	randomKey := func() key { return key{A: uint16(ops.Int(600)), B: [2]int8{int8(ops.Int(3))}} }
	model := map[key]uint64{}
	for step := 0; ops.More(); step++ {
		k := randomKey()
		switch ops.Int(20) {
		case 0:
			if err := m.Clip(); err != nil {
				t.Fatal(err)
			}
		case 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11:
			v := ops.Uint64()
			if err := m.Set(k, v); err != nil {
				t.Fatal(err)
			}
			model[k] = v
		default:
			_, want := model[k]
			if got := m.Delete(k); got != want {
				t.Fatalf("step %d: Delete(%v) = %v, want %v", step, k, got, want)
			}
			delete(model, k)
		}
		if m.Len() != len(model) {
			t.Fatalf("step %d: Len = %d, want %d", step, m.Len(), len(model))
		}
		probe := randomKey()
		got, ok := m.Get(probe)
		want, wantOK := model[probe]
		if ok != wantOK || got != want {
			t.Fatalf("step %d: Get(%v) = %d, %v; want %d, %v", step, probe, got, ok, want, wantOK)
		}
	}
	if got := maps.Collect(m.All()); !maps.Equal(got, model) {
		t.Fatal("All differs from model")
	}
	keys := 0
	for k := range m.Keys() {
		if _, ok := model[k]; !ok {
			t.Fatalf("Keys yielded %v, absent from model", k)
		}
		keys++
	}
	values := 0
	for range m.Values() {
		values++
	}
	if keys != len(model) || values != len(model) {
		t.Fatalf("Keys, Values yielded %d, %d; want %d", keys, values, len(model))
	}
}

func TestPtrUpdatesInPlace(t *testing.T) {
	t.Parallel()

	m, err := hashmap.New[int, int](memtest.Local(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	if err := m.Set(7, 1); err != nil {
		t.Fatal(err)
	}
	p, ok := m.Ptr(7)
	if !ok {
		t.Fatal("Ptr(7) missing")
	}
	*p += 41
	if got, _ := m.Get(7); got != 42 {
		t.Fatalf("Get(7) = %d, want 42", got)
	}
	if _, ok := m.Ptr(8); ok {
		t.Fatal("Ptr(8) found absent key")
	}
}

func BenchmarkSetGet(b *testing.B) {
	m, err := hashmap.New[uint64, uint64](memtest.Local(b), 0)
	if err != nil {
		b.Fatal(err)
	}
	defer m.Free()
	var i uint64
	for b.Loop() {
		if err := m.Set(i&0xffff, i); err != nil {
			b.Fatal(err)
		}
		m.Get(i * 31 & 0xffff)
		i++
	}
}

func BenchmarkGoMapSetGet(b *testing.B) {
	m := map[uint64]uint64{}
	var i uint64
	for b.Loop() {
		m[i&0xffff] = i
		_ = m[i*31&0xffff]
		i++
	}
}

func mustPanicModified(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		msg, _ := recover().(string)
		if msg != "hashmap: modified during All" {
			t.Fatalf("panic = %q, want the modified-during-All panic", msg)
		}
	}()
	f()
}

func TestStructuralChangeDuringAllPanics(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	fill := func() *hashmap.Map[int, int] {
		m, err := hashmap.New[int, int](a, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = m.Free() })
		for k := range 6 {
			if err := m.Set(k, k); err != nil {
				t.Fatal(err)
			}
		}
		return m
	}
	m := fill()
	for k := range m.All() {
		if err := m.Set(k, -k); err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range m.All() {
		if v != -k {
			t.Fatalf("m[%d] = %d after updating during All, want %d", k, v, -k)
		}
	}
	mustPanicModified(t, func() {
		for k := range m.All() {
			_ = m.Set(k+100, 0)
		}
	})
	m = fill()
	mustPanicModified(t, func() {
		for k := range m.All() {
			m.Delete(k)
		}
	})
	m = fill()
	mustPanicModified(t, func() {
		for k := range m.Keys() {
			for j := range 64 {
				_ = m.Set(1000+k*64+j, 0)
			}
		}
	})
}

func TestManyDeletionsDoNotFillTheTable(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	m, err := hashmap.New[int, int](a, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	before := a.Stats()
	for i := range 1 << 18 {
		if err := m.Set(i, i); err != nil {
			t.Fatal(err)
		}
		if !m.Delete(i) {
			t.Fatalf("Delete(%d) missed", i)
		}
		if _, ok := m.Get(i); ok {
			t.Fatalf("Get(%d) found a deleted key", i)
		}
	}
	if after := a.Stats(); after != before {
		t.Fatalf("churn grew the table: %+v -> %+v", before, after)
	}
}

func TestIntHasherAgreesWithDefault(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	m, err := hashmap.NewHashed[uint64, uint64](a, 0, hashmap.Int[uint64])
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	for i := range uint64(100_000) {
		if err := m.Set(i*i, i); err != nil {
			t.Fatal(err)
		}
	}
	for i := range uint64(100_000) {
		if got, ok := m.Get(i * i); !ok || got != i {
			t.Fatalf("Get(%d) = %d, %v", i*i, got, ok)
		}
	}
	if m.Len() != 100_000 {
		t.Fatalf("Len = %d", m.Len())
	}
}

func TestEntriesCostUnderFortyBytes(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const n = 1 << 20
	m, err := hashmap.New[uint64, uint64](a, n)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	if got := a.Stats().Bytes / n; got >= 40 {
		t.Fatalf("%d bytes per entry, want under 40", got)
	}
}

func BenchmarkIntHasherSetGet(b *testing.B) {
	a := memtest.Local(b)
	m, err := hashmap.NewHashed[int, int](a, 1<<16, hashmap.Int[int])
	if err != nil {
		b.Fatal(err)
	}
	defer m.Free()
	i := 0
	for b.Loop() {
		key := i & (1<<16 - 1)
		if err := m.Set(key, i); err != nil {
			b.Fatal(err)
		}
		if _, ok := m.Get(key); !ok {
			b.Fatal("missing key")
		}
		i++
	}
}

func TestGetManyMatchesGet(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	m, err := hashmap.New[uint64, uint64](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	for i := range uint64(10_000) {
		if err := m.Set(i*2, i); err != nil {
			t.Fatal(err)
		}
	}
	keys := make([]uint64, 1003)
	for i := range keys {
		keys[i] = uint64(i) * 7
	}
	values, found := make([]uint64, len(keys)), make([]bool, len(keys))
	for i := range values {
		values[i] = 99
	}
	m.GetMany(keys, values, found)
	for i, key := range keys {
		want, wantOK := m.Get(key)
		if values[i] != want || found[i] != wantOK {
			t.Fatalf("key %d: %d, %v; want %d, %v", key, values[i], found[i], want, wantOK)
		}
	}
	m.GetMany(nil, nil, nil)
}

func benchLookups(b *testing.B, size int, batched bool) {
	a := &memory.Local{}
	defer a.Close()
	m, err := hashmap.NewHashed[uint64, uint64](a, size, hashmap.Int[uint64])
	if err != nil {
		b.Fatal(err)
	}
	for i := range uint64(size) {
		if err := m.Set(i, i); err != nil {
			b.Fatal(err)
		}
	}
	const batch = 256
	keys, values, found := make([]uint64, batch), make([]uint64, batch), make([]bool, batch)
	x := uint64(88172645463325252)
	for b.Loop() {
		for i := range keys {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			keys[i] = x % uint64(size)
		}
		if batched {
			m.GetMany(keys, values, found)
			continue
		}
		for i, key := range keys {
			values[i], found[i] = m.Get(key)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*batch), "ns/key")
}

func BenchmarkLookupLoop16M(b *testing.B)    { benchLookups(b, 16<<20, false) }
func BenchmarkLookupGetMany16M(b *testing.B) { benchLookups(b, 16<<20, true) }
func BenchmarkLookupLoop64K(b *testing.B)    { benchLookups(b, 64<<10, false) }
func BenchmarkLookupGetMany64K(b *testing.B) { benchLookups(b, 64<<10, true) }

func TestGetOrSetAndUpsert(t *testing.T) {
	t.Parallel()

	m, err := hashmap.New[string2, int](memtest.Local(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	key := string2{1, 2}
	got, loaded, err := m.GetOrSet(key, 5)
	if err != nil || loaded || got != 5 {
		t.Fatalf("GetOrSet on absent key = %d, %v, %v", got, loaded, err)
	}
	got, loaded, err = m.GetOrSet(key, 6)
	if err != nil || !loaded || got != 5 {
		t.Fatalf("GetOrSet on present key = %d, %v, %v", got, loaded, err)
	}
	add := func(old int, found bool) int {
		if found {
			return old + 10
		}
		return 1
	}
	if err := m.Upsert(key, add); err != nil {
		t.Fatal(err)
	}
	other := string2{3, 4}
	if err := m.Upsert(other, add); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Get(key); v != 15 {
		t.Fatalf("after Upsert of a present key = %d", v)
	}
	if v, _ := m.Get(other); v != 1 {
		t.Fatalf("after Upsert of an absent key = %d", v)
	}
	if !m.Contains(key) || m.Contains(string2{9, 9}) {
		t.Fatal("Contains is wrong")
	}
}

func TestClearEmptiesAndStaysUsable(t *testing.T) {
	t.Parallel()

	m, err := hashmap.NewHashed[uint64, uint64](memtest.Local(t), 0, hashmap.Int[uint64])
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	for i := range uint64(1000) {
		if err := m.Set(i, i); err != nil {
			t.Fatal(err)
		}
	}
	for i := range uint64(500) {
		m.Delete(i)
	}
	m.Clear()
	if m.Len() != 0 || m.Contains(600) {
		t.Fatalf("Len after Clear = %d", m.Len())
	}
	for range m.All() {
		t.Fatal("All yielded after Clear")
	}
	for i := range uint64(2000) {
		if err := m.Set(i, i+1); err != nil {
			t.Fatal(err)
		}
	}
	for i := range uint64(2000) {
		if v, ok := m.Get(i); !ok || v != i+1 {
			t.Fatalf("Get(%d) = %d, %v", i, v, ok)
		}
	}
}

func TestStringsClearKeysValuesContains(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	m, err := hashmap.NewStrings[int](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	want := map[string]int{"a": 1, "b": 2, "c": 3}
	for k, v := range want {
		if err := m.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if !m.Contains("a") || m.Contains("z") {
		t.Fatal("Contains is wrong")
	}
	keys, sum := 0, 0
	for k := range m.Keys() {
		if _, ok := want[k]; !ok {
			t.Fatalf("Keys yielded %q", k)
		}
		keys++
	}
	for v := range m.Values() {
		sum += v
	}
	if keys != 3 || sum != 6 {
		t.Fatalf("Keys saw %d, Values summed to %d", keys, sum)
	}
	if err := m.Clear(); err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 || m.Contains("a") {
		t.Fatal("not empty after Clear")
	}
	if err := m.Set("a", 9); err != nil {
		t.Fatal(err)
	}
	if v, ok := m.Get("a"); !ok || v != 9 {
		t.Fatalf("Get after Clear = %d, %v", v, ok)
	}
}

type string2 struct{ A, B int }
