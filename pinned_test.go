package memory_test

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
)

func collectUntil(t *testing.T, what string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !done(); {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen after collecting for 10s", what)
		}
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
}

type pinBox struct {
	Data [64]byte
	Next *int
}

type pinHolder struct {
	P memory.Pinned[pinBox]
	N uintptr
}

//go:noinline
func pinFresh(ps *memory.Pins, collected *atomic.Bool) memory.Pinned[pinBox] {
	n := 7
	b := &pinBox{Next: &n}
	for i := range b.Data {
		b.Data[i] = byte(i)
	}
	runtime.AddCleanup(b, func(c *atomic.Bool) { c.Store(true) }, collected)
	return memory.Pin(ps, b)
}

func checkBox(t *testing.T, b *pinBox) {
	t.Helper()
	for i, v := range b.Data {
		if v != byte(i) {
			t.Fatalf("Data[%d] = %d, want %d", i, v, i)
		}
	}
	if *b.Next != 7 {
		t.Fatalf("*Next = %d, want 7", *b.Next)
	}
}

func TestPinnedSurvivesGCOnlyReferencedOffHeap(t *testing.T) {
	a := memtest.Local(t)
	var ps memory.Pins
	cell, err := memory.New[pinHolder](a)
	if err != nil {
		t.Fatal(err)
	}
	var collected atomic.Bool
	cell.Value().P = pinFresh(&ps, &collected)
	for range 10 {
		runtime.GC()
		_ = make([]byte, 1<<16)
	}
	time.Sleep(10 * time.Millisecond)
	if collected.Load() {
		t.Fatal("pinned object was collected while pinned")
	}
	checkBox(t, cell.Value().P.Value())
	if ps.Len() != 1 {
		t.Fatalf("Len = %d, want 1", ps.Len())
	}
	memory.Unpin(&ps, &cell.Value().P)
	if !cell.Value().P.IsNil() {
		t.Fatal("Unpin left the handle set")
	}
	if ps.Len() != 0 {
		t.Fatalf("Len after Unpin = %d, want 0", ps.Len())
	}
	collectUntil(t, "collecting the unpinned object", collected.Load)
	if err := memory.Free(a, cell); err != nil {
		t.Fatal(err)
	}
}

func TestPinCountsRepeatedPins(t *testing.T) {
	t.Parallel()

	var ps memory.Pins
	x := new(int)
	p, q := memory.Pin(&ps, x), memory.Pin(&ps, x)
	if p.Value() != x || q.Value() != x {
		t.Fatal("Value does not return the pinned pointer")
	}
	memory.Unpin(&ps, &p)
	if ps.Len() != 1 {
		t.Fatalf("Len = %d after one of two Unpins, want 1", ps.Len())
	}
	memory.Unpin(&ps, &p)
	memory.Unpin(&ps, &q)
	if ps.Len() != 0 {
		t.Fatalf("Len = %d, want 0", ps.Len())
	}
	if n := memory.Pin[int](&ps, nil); !n.IsNil() || ps.Len() != 0 {
		t.Fatal("pinning nil pinned something")
	}
}

func TestUnpinOfUnpinnedPanics(t *testing.T) {
	t.Parallel()

	var ps memory.Pins
	p := memory.Pin(&ps, new(int))
	stale := p
	memory.Unpin(&ps, &p)
	mustPanic(t, "not pinned here", func() { memory.Unpin(&ps, &stale) })
	var other memory.Pins
	q := memory.Pin(&other, new(int))
	defer other.UnpinAll()
	mustPanic(t, "not pinned here", func() { memory.Unpin(&ps, &q) })
}

func TestUnpinAllReleases(t *testing.T) {
	var ps memory.Pins
	var collected atomic.Bool
	_ = pinFresh(&ps, &collected)
	_ = memory.Pin(&ps, new(int))
	if ps.Len() != 2 {
		t.Fatalf("Len = %d, want 2", ps.Len())
	}
	ps.UnpinAll()
	if ps.Len() != 0 {
		t.Fatalf("Len after UnpinAll = %d, want 0", ps.Len())
	}
	collectUntil(t, "collecting after UnpinAll", collected.Load)
}

func TestPinnedIsAllowedOffHeapAndZeroedWhenUninit(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	dirty, err := memory.New[[2]uintptr](a)
	if err != nil {
		t.Fatal(err)
	}
	*dirty.Value() = [2]uintptr{0x5a5a5a5a, 0x5a5a5a5a}
	if err := memory.Free(a, dirty); err != nil {
		t.Fatal(err)
	}
	p, err := memory.NewUninit[pinHolder](a)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Value().P.IsNil() {
		t.Fatal("NewUninit of a type holding Pinned left stale bytes")
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
}

func TestPinningIntoOffHeapDuringGC(t *testing.T) {
	a := memtest.Local(t)
	var ps memory.Pins
	whileCollecting(t, func([]uintptr) {
		for i := range gcRounds / 4 {
			cell, err := memory.NewUninit[pinHolder](a)
			if err != nil {
				t.Fatal(err)
			}
			n := i
			b := &pinBox{Next: &n}
			cell.Value().P = memory.Pin(&ps, b)
			cell.Value().N = uintptr(i)
			if got := *cell.Value().P.Value().Next; got != i {
				t.Fatalf("round %d read %d", i, got)
			}
			memory.Unpin(&ps, &cell.Value().P)
			if err := memory.Free(a, cell); err != nil {
				t.Fatal(err)
			}
		}
	})
	if ps.Len() != 0 {
		t.Fatalf("Len = %d, want 0", ps.Len())
	}
}
