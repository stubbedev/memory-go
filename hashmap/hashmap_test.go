package hashmap_test

import (
	"errors"
	"maps"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/internal/memtest"
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
	f.Add(memtest.Seed(3, 1<<16))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := hashmap.New[key, uint64](memtest.Local(t), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer m.Free()
		ops := memtest.NewOps(data)
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
	})
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
