package memory_test

import (
	"errors"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/vector"
)

func newArena(t *testing.T) *memory.Arena {
	t.Helper()
	a, err := memory.NewArena(memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Free(); err != nil {
			t.Error(err)
		}
	})
	return a
}

func TestArenaHoldsMixedTypes(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	p, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	s, err := memory.NewString(a, "hello")
	if err != nil {
		t.Fatal(err)
	}
	xs, err := memory.MakeSlice[uint16](a, 1000)
	if err != nil {
		t.Fatal(err)
	}
	*p.Value() = point{X: 1}
	xs.Elems()[999] = 7
	if p.Value().X != 1 || s.String() != "hello" || xs.Elems()[999] != 7 {
		t.Fatal("values overlap")
	}
}

func TestArenaGrowsLastAllocationInPlace(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	s, err := memory.MakeSlice[uint64](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	first := &s.Elems()[0]
	s.Elems()[3] = 9
	if s, err = memory.ResizeSlice(a, s, 100); err != nil {
		t.Fatal(err)
	}
	if &s.Elems()[0] != first {
		t.Fatal("resizing the last allocation moved it")
	}
	if s.Elems()[3] != 9 || s.Elems()[99] != 0 {
		t.Fatalf("contents after resize: %d, %d", s.Elems()[3], s.Elems()[99])
	}
}

func TestArenaResetReusesAndZeroes(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	for round := range 5 {
		for i := range 40_000 {
			p, err := memory.New[point](a)
			if err != nil {
				t.Fatal(err)
			}
			if *p.Value() != (point{}) {
				t.Fatalf("round %d allocation %d not zeroed", round, i)
			}
			*p.Value() = point{X: -1, Y: -1}
		}
		if round == 0 {
			continue
		}
		before := a.Footprint().Mapped
		if err := a.Reset(); err != nil {
			t.Fatal(err)
		}
		if got := a.Footprint().Mapped; got != before {
			t.Fatalf("Reset changed Mapped from %d to %d", before, got)
		}
		if got := a.Stats(); got != (memory.Stats{}) {
			t.Fatalf("Stats after Reset = %+v", got)
		}
	}
}

func TestArenaMarkRelease(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	keep, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	keep.Value().X = 42
	m := a.Mark()
	before := a.Stats()
	for range 100_000 {
		if _, err := memory.New[point](a); err != nil {
			t.Fatal(err)
		}
	}
	big, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	big.Elems()[0] = 1
	if err := a.Release(m); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != before {
		t.Fatalf("Stats after Release = %+v, want %+v", got, before)
	}
	if keep.Value().X != 42 {
		t.Fatal("Release lost an allocation made before the Mark")
	}
	next, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	if *next.Value() != (point{}) {
		t.Fatalf("reused memory not zeroed: %+v", *next.Value())
	}
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("Release to a stale Mark did not panic")
		}
	}()
	_ = a.Release(m)
}

func TestContainersRunOnAnArena(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	v, err := vector.New[uint64](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	m, err := hashmap.NewStrings[uint64](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	const n = 50_000
	for i := range uint64(n) {
		if err := v.Push(i); err != nil {
			t.Fatal(err)
		}
		if err := m.Set(string(rune('a'+i%26))+string(rune(i)), i); err != nil {
			t.Fatal(err)
		}
	}
	for i := range n {
		if v.At(i) != uint64(i) {
			t.Fatalf("v[%d] = %d", i, v.At(i))
		}
	}
	if m.Len() == 0 {
		t.Fatal("empty map")
	}
	got, ok := m.Get(string(rune('a'+7)) + string(rune(7)))
	if !ok || got != 7 {
		t.Fatalf("Get = %d, %v", got, ok)
	}
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
}

func TestArenaFreeReturnsSpans(t *testing.T) {
	t.Parallel()

	parent := memtest.Local(t)
	a, err := memory.NewArena(parent)
	if err != nil {
		t.Fatal(err)
	}
	for range 100_000 {
		if _, err := memory.New[point](a); err != nil {
			t.Fatal(err)
		}
	}
	big, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if big.Len() != 1<<20 {
		t.Fatal("wrong length")
	}
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
	if got := parent.Stats(); got != (memory.Stats{}) {
		t.Fatalf("parent Stats = %+v", got)
	}
	if got := a.Footprint(); got != (memory.Footprint{}) {
		t.Fatalf("arena Footprint after Free = %+v", got)
	}
	p, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	if p.IsNil() {
		t.Fatal("arena unusable after Free")
	}
	if err := errors.Join(a.Free(), parent.Trim()); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkArenaNewReset(b *testing.B) {
	a, err := memory.NewArena(memtest.Local(b))
	if err != nil {
		b.Fatal(err)
	}
	defer a.Free()
	for b.Loop() {
		for range 1024 {
			if _, err := memory.New[point](a); err != nil {
				b.Fatal(err)
			}
		}
		if err := a.Reset(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*1024), "ns/alloc")
}
