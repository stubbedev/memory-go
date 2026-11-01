package memory_test

import (
	"errors"
	"testing"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func TestArenaResizeAfterMarkLeavesTheOriginalInPlace(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	s, err := memory.MakeSlice[uint64](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	s.Elems()[0] = 7
	m := a.Mark()
	grown, err := memory.ResizeSlice(a, s, 100)
	if err != nil {
		t.Fatal(err)
	}
	if unsafe.SliceData(grown.Elems()) == unsafe.SliceData(s.Elems()) {
		t.Fatal("resize after a Mark grew an allocation from before it in place")
	}
	grown.Elems()[0] = 9
	if err := a.Release(m); err != nil {
		t.Fatal(err)
	}
	mustPanicIfChecked(t, func() { grown.Elems() })
	if got := s.Elems()[0]; got != 7 {
		t.Fatalf("original[0] = %d after Release, want 7", got)
	}
	next, err := memory.MakeSlice[uint64](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	next.Elems()[0] = 11
	if got := s.Elems()[0]; got != 7 {
		t.Fatalf("original[0] = %d after the next allocation, want 7", got)
	}
}

func TestArenaGrowthAcrossMarkKeepsTheData(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	s, err := memory.MakeSlice[uint64](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	s.Elems()[3] = 7
	m := a.Mark()
	grown, err := memory.ResizeSlice(a, s, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := grown.Elems()[3]; got != 7 {
		t.Fatalf("grown[3] = %d, want 7", got)
	}
	if err := a.Release(m); err != nil {
		t.Fatal(err)
	}
}

func TestArenaReleaseAfterFreeingFromBeforeTheMark(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	small, err := memory.New[uint64](a)
	if err != nil {
		t.Fatal(err)
	}
	big, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m := a.Mark()
	if err := memory.Free(a, small); err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeSlice(a, big); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(m); err != nil {
		t.Fatal(err)
	}
}

func TestArenaReleaseFreesLargeAllocationsAfterTheMark(t *testing.T) {
	t.Parallel()

	parent := memtest.Local(t)
	a := memory.NewArena(parent)
	first, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m := a.Mark()
	if err := memory.FreeSlice(a, first); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.MakeSlice[byte](a, 2<<20); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(m); err != nil {
		t.Fatal(err)
	}
	if got := parent.Stats().Live; got != 1 {
		t.Fatalf("parent holds %d allocations after Release, want only the one from before the Mark", got)
	}
	second.Elems()[0] = 1
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
}

func TestArenaResizedLargeAllocationIsReleased(t *testing.T) {
	t.Parallel()

	parent := memtest.Local(t)
	a := memory.NewArena(parent)
	s, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	m := a.Mark()
	if _, err := memory.ResizeSlice(a, s, 2<<20); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(m); err != nil {
		t.Fatal(err)
	}
	if got := parent.Stats().Live; got != 0 {
		t.Fatalf("parent holds %d allocations after Release, want 0", got)
	}
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
}

func TestArenaStaleMarksPanic(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	mustPanic(t, "no longer valid", func() { _ = a.Release(memory.Mark{}) })

	other := newArena(t)
	foreign := other.Mark()
	mustPanic(t, "no longer valid", func() { _ = a.Release(foreign) })

	beforeReset := a.Mark()
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "no longer valid", func() { _ = a.Release(beforeReset) })

	outer := a.Mark()
	if _, err := memory.MakeSlice[byte](a, 1<<20); err != nil {
		t.Fatal(err)
	}
	inner := a.Mark()
	if err := a.Release(outer); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "no longer valid", func() { _ = a.Release(inner) })
}

func TestArenaFootprintDoesNotDoubleCount(t *testing.T) {
	t.Parallel()

	parent := memtest.Local(t)
	a := memory.NewArena(parent)
	const large = 64 << 20
	if _, err := memory.MakeSlice[byte](a, large); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.New[uint64](a); err != nil {
		t.Fatal(err)
	}
	const slack = 32 << 20
	arena, held := a.Footprint(), parent.Footprint()
	if got := arena.Committed + held.Committed; got < large || got > large+1<<20 {
		t.Fatalf("Committed sums to %d for %d bytes allocated", got, large)
	}
	if got := arena.Mapped + held.Mapped; got < large || got > large+slack {
		t.Fatalf("Mapped sums to %d for %d bytes allocated", got, large)
	}
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
}

func TestArenaStatsCountLargeAllocations(t *testing.T) {
	t.Parallel()

	a := newArena(t)
	big, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != (memory.Stats{Live: 1, Bytes: 1 << 20}) {
		t.Fatalf("Stats = %+v", got)
	}
	if err := memory.FreeSlice(a, big); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != (memory.Stats{}) {
		t.Fatalf("Stats after free = %+v", got)
	}
}

func TestArenaForeignLargeResizePanicsWithoutTouchingIt(t *testing.T) {
	t.Parallel()

	parent := memtest.Local(t)
	a := memory.NewArena(parent)
	defer a.Free()
	mine, err := memory.MakeSlice[byte](parent, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "not a live allocation", func() { _, _ = memory.ResizeSlice(a, mine, 2<<20) })
	if mine.Len() != 1<<20 || len(mine.Elems()) != 1<<20 {
		t.Fatal("foreign allocation changed")
	}
	if err := memory.FreeSlice(parent, mine); err != nil {
		t.Fatal(err)
	}
}

func TestWithArenaFreesOnEveryExit(t *testing.T) {
	t.Parallel()

	parent := memtest.Local(t)
	sentinel := errors.New("stop")
	err := memory.WithArena(parent, func(a *memory.Arena) error {
		if _, err := memory.MakeSlice[byte](a, 1<<20); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
	if got := parent.Stats(); got != (memory.Stats{}) {
		t.Fatalf("parent Stats after error = %+v", got)
	}
	mustPanic(t, "boom", func() {
		_ = memory.WithArena(parent, func(a *memory.Arena) error {
			if _, err := memory.MakeSlice[byte](a, 1<<20); err != nil {
				t.Fatal(err)
			}
			panic("boom")
		})
	})
	if got := parent.Stats(); got != (memory.Stats{}) {
		t.Fatalf("parent Stats after panic = %+v", got)
	}
}

func mustPanicIfChecked(t *testing.T, f func()) {
	t.Helper()
	if !memory.Checked {
		return
	}
	mustPanic(t, "after it was freed", f)
}
