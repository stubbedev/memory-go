package memory_test

import (
	"os"
	"runtime/debug"
	"sync"
	"testing"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
)

var faultSink byte

func mustFault(t *testing.T, f func()) {
	t.Helper()
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		t.Helper()
		r := recover()
		if _, ok := r.(interface{ Addr() uintptr }); !ok {
			t.Fatalf("recovered %v, want a memory fault", r)
		}
	}()
	f()
	t.Fatal("no fault")
}

func onlyChecked(t *testing.T) {
	t.Helper()
	if !memory.Checked {
		t.Skip("handle checks are compiled out")
	}
}

func TestGuardedValueAfterFreePanics(t *testing.T) {
	t.Parallel()
	onlyChecked(t)

	a := memtest.Guarded(t)
	p, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	*p.Value() = 7
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "after it was freed", func() { p.Value() })
}

func TestGuardedLargeElemsAfterFreePanics(t *testing.T) {
	t.Parallel()
	onlyChecked(t)

	a := memtest.Guarded(t)
	for _, n := range []int{9000, 3 << 12, 1 << 20} {
		s, err := memory.MakeSlice[byte](a, n)
		if err != nil {
			t.Fatal(err)
		}
		e := s.Elems()
		e[0], e[n-1] = 1, 2
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
		mustPanic(t, "after it was freed", func() { s.Elems() })
	}
}

func TestGuardedRawPointerAfterFreeFaults(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	p, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	raw := p.Value()
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	mustFault(t, func() { *raw = 1 })
}

func TestGuardedRefAndViewAfterFreeFault(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	s, err := memory.MakeSlice[uint32](a, 16)
	if err != nil {
		t.Fatal(err)
	}
	r, v := s.Ref(3), s.View(2, 5)
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
	mustFault(t, func() { faultSink = byte(*r.Value()) })
	mustFault(t, func() { faultSink = byte(v.Elems()[0]) })
}

func TestGuardedOverrunFaults(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	for _, n := range []int{1, 10, 4096, 9000, 3 << 12} {
		s, err := memory.MakeSlice[byte](a, n)
		if err != nil {
			t.Fatal(err)
		}
		e := s.Elems()
		e[n-1] = 1
		past := (*byte)(unsafe.Add(unsafe.Pointer(&e[0]), n))
		mustFault(t, func() { *past = 1 })
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuardedRespectsAlignment(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	for _, align := range []int{8, 64, 4096} {
		s, err := memory.MakeSliceAligned[byte](a, 10, align)
		if err != nil {
			t.Fatal(err)
		}
		if addr := uintptr(unsafe.Pointer(&s.Elems()[0])); addr%uintptr(align) != 0 {
			t.Errorf("align %d: address %#x", align, addr)
		}
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuardedMisusedFreesPanic(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	p, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "is already freed", func() { _ = memory.Free(a, p) })

	var l memory.Local
	defer l.Close()
	q, err := memory.New[int](&l)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "not a live allocation of this Guarded allocator", func() { _ = memory.Free(a, q) })
	if err := memory.Free(&l, q); err != nil {
		t.Fatal(err)
	}

	big, err := memory.MakeSlice[byte](a, 10000)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "not a live allocation", func() { _ = memory.FreeSlice(&l, big) })
	mustPanic(t, "allocated as 10000 bytes, not 40000", func() {
		_ = memory.FreeSlice(a, *(*memory.Slice[uint32])(unsafe.Pointer(&big)))
	})
	if err := memory.FreeSlice(a, big); err != nil {
		t.Fatal(err)
	}
	if memory.Checked {
		small, err := memory.New[int](a)
		if err != nil {
			t.Fatal(err)
		}
		mustPanic(t, "belongs to a different allocator", func() { _ = memory.Free(&l, small) })
		if err := memory.Free(a, small); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuardedResizeMovesAndQuarantines(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	for _, n := range []int{16, 9000} {
		s, err := memory.MakeSlice[byte](a, n)
		if err != nil {
			t.Fatal(err)
		}
		old := s.Elems()
		old[0] = 42
		grown, err := memory.ResizeSlice(a, s, n)
		if err != nil {
			t.Fatal(err)
		}
		if unsafe.SliceData(grown.Elems()) == unsafe.SliceData(old) || grown.Elems()[0] != 42 {
			t.Fatalf("size %d: resize stayed in place or lost data", n)
		}
		mustFault(t, func() { faultSink = old[0] })
		if memory.Checked {
			mustPanic(t, "after it was resized", func() { s.Elems() })
		}
		mustPanic(t, "was resized since this handle was made", func() { _ = memory.FreeSlice(a, s) })
		if err := memory.FreeSlice(a, grown); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuardedQuarantineUnmapsOldest(t *testing.T) {
	t.Parallel()

	page := os.Getpagesize()
	block := 3*page + 3*page
	a := memory.NewGuarded(5 * page)
	defer a.Close()
	var slices [3]memory.Slice[byte]
	for i := range slices {
		s, err := memory.MakeSlice[byte](a, 3*page)
		if err != nil {
			t.Fatal(err)
		}
		slices[i] = s
	}
	if got := a.Footprint().Mapped; got != 3*block {
		t.Fatalf("Mapped = %d, want %d", got, 3*block)
	}
	for _, s := range slices[:2] {
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := a.Footprint(), (memory.Footprint{Mapped: 2 * block, Committed: 2*page + 3*page, Retained: 4 * page}); got != want {
		t.Fatalf("Footprint = %+v, want %+v", got, want)
	}
	mustPanic(t, "freed and since unmapped", func() { _ = memory.FreeSlice(a, slices[0]) })
	mustPanic(t, "is already freed", func() { _ = memory.FreeSlice(a, slices[1]) })
	if err := memory.FreeSlice(a, slices[2]); err != nil {
		t.Fatal(err)
	}
	if got := a.Footprint(); got.Mapped != block || got.Retained != 4*page {
		t.Fatalf("Footprint = %+v after freeing everything", got)
	}
}

func TestGuardedStatsAndFootprint(t *testing.T) {
	t.Parallel()

	const largestSmallAllocation = 8 << 10
	page := os.Getpagesize()
	sliceLen := min(page+1, largestSmallAllocation)
	slicePages := (sliceLen + page - 1) / page
	var a memory.Guarded
	p, err := memory.New[uint64](&a)
	if err != nil {
		t.Fatal(err)
	}
	s, err := memory.MakeSlice[byte](&a, sliceLen)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := a.Stats(), (memory.Stats{Live: 2, Bytes: 8 + sliceLen}); got != want {
		t.Fatalf("Stats = %+v, want %+v", got, want)
	}
	if got, want := a.Footprint(), (memory.Footprint{Mapped: 256 << 10, Committed: (2 + slicePages) * page}); got != want {
		t.Fatalf("Footprint = %+v, want %+v", got, want)
	}
	if err := memory.Free(&a, p); err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeSlice(&a, s); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != (memory.Stats{}) {
		t.Fatalf("Stats = %+v after freeing everything", got)
	}
	if got, want := a.Footprint(), (memory.Footprint{Mapped: 256 << 10, Committed: page, Retained: (3 + slicePages) * page}); got != want {
		t.Fatalf("Footprint = %+v, want %+v", got, want)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a.Footprint(); got != (memory.Footprint{}) {
		t.Fatalf("Footprint = %+v after Close", got)
	}
	if _, err := memory.New[int](&a); err != nil {
		t.Fatalf("New after Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGuardedConcurrentUse(t *testing.T) {
	t.Parallel()

	a := memtest.Guarded(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 200 {
				s, err := memory.MakeSlice[byte](a, 1+i*97%12000)
				if err != nil {
					t.Error(err)
					return
				}
				s.Elems()[0] = byte(i)
				if err := memory.FreeSlice(a, s); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
}
