//go:build !memory_unchecked

package memory_test

import (
	"strings"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want one containing %q", r, want)
		}
	}()
	f()
}

func TestDoubleFreePanics(t *testing.T) {
	t.Parallel()

	for _, n := range []int{1, 1 << 20} {
		a := memtest.Local(t)
		s, err := memory.MakeSlice[byte](a, n)
		if err != nil {
			t.Fatal(err)
		}
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
		mustPanic(t, "already freed", func() { _ = memory.FreeSlice(a, s) })
	}
}

func TestUseAfterFreePanics(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	big, err := memory.MakeSlice[int](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	str, err := memory.NewString(a, "gone")
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeSlice(a, big); err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeString(a, str); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "after it was freed", func() { p.Value() })
	mustPanic(t, "after it was freed", func() { p.Ref() })
	mustPanic(t, "after it was freed", func() { big.Elems() })
	mustPanic(t, "after it was freed", func() { _ = str.String() })
}

func TestForeignAllocatorPanics(t *testing.T) {
	t.Parallel()

	a, b := memtest.Local(t), memtest.Local(t)
	s, err := memory.MakeSlice[int](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "belongs to a different allocator", func() { _ = memory.FreeSlice(b, s) })
	mustPanic(t, "belongs to a different allocator", func() { _, _ = memory.ResizeSlice(b, s, 8) })
	if s, err = memory.ResizeSlice(a, s, 8); err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestResizeInvalidatesOldHandle(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	grown, err := memory.ResizeSlice(a, s, 1<<22)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "already freed", func() { _ = memory.FreeSlice(a, s) })
	if err := memory.FreeSlice(a, grown); err != nil {
		t.Fatal(err)
	}
}

func TestForeignAllocatorPanicsOnLargeAllocation(t *testing.T) {
	t.Parallel()

	a, b := memtest.Local(t), memtest.Local(t)
	s, err := memory.MakeSlice[byte](a, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "not a live allocation of this allocator", func() { _ = memory.FreeSlice(b, s) })
	mustPanic(t, "not a live allocation of this allocator", func() { _, _ = memory.ResizeSlice(b, s, 1) })
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestInPlaceResizeInvalidatesOldHandle(t *testing.T) {
	t.Parallel()

	for _, n := range []int{100, 4 << 20} {
		a := memtest.Local(t)
		s, err := memory.MakeSlice[byte](a, n)
		if err != nil {
			t.Fatal(err)
		}
		shrunk, err := memory.ResizeSlice(a, s, n-10)
		if err != nil {
			t.Fatal(err)
		}
		mustPanic(t, "was resized", func() { _ = memory.FreeSlice(a, s) })
		mustPanic(t, "was resized", func() { _, _ = memory.ResizeSlice(a, s, n) })
		mustPanic(t, "after it was resized", func() { s.Elems() })
		if len(shrunk.Elems()) != n-10 {
			t.Fatalf("len = %d, want %d", len(shrunk.Elems()), n-10)
		}
		if err := memory.FreeSlice(a, shrunk); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAppendStringInvalidatesOldString(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.NewString(a, "ab")
	if err != nil {
		t.Fatal(err)
	}
	grown, err := memory.AppendString(a, s, "c")
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "after it was resized", func() { _ = s.String() })
	mustPanic(t, "was resized", func() { _ = memory.FreeString(a, s) })
	if grown.String() != "abc" {
		t.Fatalf("grown = %q, want abc", grown.String())
	}
	if err := memory.FreeString(a, grown); err != nil {
		t.Fatal(err)
	}
}
