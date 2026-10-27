package hashmap_test

import (
	"errors"
	"maps"
	"math/rand/v2"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/internal/memtest"
)

type key struct {
	A uint16
	B [2]int8
}

func TestRejectsPointerTypes(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := hashmap.New[string, int](a); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Errorf("string key: err = %v, want ErrHoldsPointers", err)
	}
	if _, err := hashmap.New[int, []byte](a); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Errorf("slice value: err = %v, want ErrHoldsPointers", err)
	}
}

func TestMatchesMap(t *testing.T) {
	t.Parallel()

	m, err := hashmap.New[key, uint64](memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(3, 4))
	randomKey := func() key { return key{A: uint16(rng.IntN(600)), B: [2]int8{int8(rng.IntN(3))}} }
	model := map[key]uint64{}
	for step := range 50_000 {
		k := randomKey()
		switch rng.IntN(3) {
		case 0, 1:
			v := rng.Uint64()
			if err := m.Set(k, v); err != nil {
				t.Fatal(err)
			}
			model[k] = v
		case 2:
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
	if err := m.Free(); err != nil {
		t.Fatal(err)
	}
}

func TestPtrUpdatesInPlace(t *testing.T) {
	t.Parallel()

	m, err := hashmap.New[int, int](memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
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
	m, err := hashmap.New[uint64, uint64](memtest.Local(b))
	if err != nil {
		b.Fatal(err)
	}
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
