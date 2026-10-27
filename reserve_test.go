package memory_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

type statser interface {
	memory.Allocator
	Stats() memory.Stats
}

var reserveSizes = []struct {
	name string
	n    int
}{
	{"4KiB", 4 << 10},
	{"8KiB", 8 << 10},
	{"16KiB", 16 << 10},
	{"64KiB", 64 << 10},
	{"256x256 RGBA", 256 * 256 * 4},
	{"512 rows at stride 2048", 512 * 2048},
	{"1MiB", 1 << 20},
	{"4MiB-1", 4<<20 - 1},
	{"1024x1024 RGBA", 1024 * 1024 * 4},
	{"4MiB+1", 4<<20 + 1},
	{"1920x1080 RGBA", 1920 * 1080 * 4},
	{"3840x2160 RGBA", 3840 * 2160 * 4},
}

func allocators(t *testing.T) map[string]statser {
	t.Helper()
	shared := &memory.Shared{}
	t.Cleanup(func() {
		if err := shared.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return map[string]statser{"Local": memtest.Local(t), "Shared": shared}
}

func TestReservationWithinOnePage(t *testing.T) {
	t.Parallel()

	page := os.Getpagesize()
	for name, a := range allocators(t) {
		for _, tc := range reserveSizes {
			s, err := memory.MakeSlice[byte](a, tc.n)
			if err != nil {
				t.Fatalf("%s %s: %v", name, tc.name, err)
			}
			got := a.Stats().Bytes
			if got < tc.n || got > tc.n+page {
				t.Errorf("%s %s: %d bytes reserved for %d, want [%d, %d]", name, tc.name, got, tc.n, tc.n, tc.n+page)
			}
			if err := memory.FreeSlice(a, s); err != nil {
				t.Fatalf("%s %s: %v", name, tc.name, err)
			}
			if got := a.Stats(); got != (memory.Stats{}) {
				t.Errorf("%s %s: Stats after free = %+v, want zero", name, tc.name, got)
			}
		}
	}
}

func TestResizeAcrossSizeClassesKeepsContents(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var s memory.Slice[byte]
	sizes := []int{100, 9 << 10, 4 << 20, 4<<20 + 100, 4<<20 + 5000, 64 << 10, 3000, 1 << 20}
	prev := 0
	for _, n := range sizes {
		var err error
		if s, err = memory.ResizeSlice(a, s, n); err != nil {
			t.Fatalf("resize to %d: %v", n, err)
		}
		e := s.Elems()
		for i := range min(prev, n) {
			if e[i] != byte(i*7) {
				t.Fatalf("resize %d to %d: s[%d] = %d, want %d", prev, n, i, e[i], byte(i*7))
			}
		}
		for i := prev; i < n; i++ {
			if e[i] != 0 {
				t.Fatalf("resize %d to %d: s[%d] = %d, want 0", prev, n, i, e[i])
			}
		}
		for i := range e {
			e[i] = byte(i * 7)
		}
		prev = n
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestLargeReuseIsZeroed(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	for range 3 {
		s, err := memory.MakeSlice[byte](a, 1920*1080*4)
		if err != nil {
			t.Fatal(err)
		}
		for i, v := range s.Elems() {
			if v != 0 {
				t.Fatalf("s[%d] = %d, want 0", i, v)
			}
		}
		for i := range s.Elems() {
			s.Elems()[i] = 0xff
		}
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Trim(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseReleasesLiveLargeAllocations(t *testing.T) {
	t.Parallel()

	var a memory.Local
	if _, err := memory.MakeSlice[byte](&a, 4<<20); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != (memory.Stats{}) {
		t.Fatalf("Stats after Close = %+v, want zero", got)
	}
	s, err := memory.MakeSlice[byte](&a, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(memory.FreeSlice(&a, s), a.Close()); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkMakeFreeFrame(b *testing.B) {
	a := memtest.Local(b)
	for b.Loop() {
		s, err := memory.MakeSlice[byte](a, 1920*1080*4)
		if err != nil {
			b.Fatal(err)
		}
		if err := memory.FreeSlice(a, s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMakeFree64KiB(b *testing.B) {
	a := memtest.Local(b)
	for b.Loop() {
		s, err := memory.MakeSlice[byte](a, 64<<10)
		if err != nil {
			b.Fatal(err)
		}
		if err := memory.FreeSlice(a, s); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStaleLargeHandlePanicsInEveryBuild(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.MakeSlice[byte](a, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "not a live allocation of this allocator") {
			t.Fatalf("panic = %q, want not-live panic", msg)
		}
	}()
	_ = memory.FreeSlice(a, s)
}
