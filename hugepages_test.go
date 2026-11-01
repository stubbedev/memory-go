package memory_test

import (
	"testing"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func TestHugePagesAlignLargeAllocations(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	a.UseHugePages()
	const huge = 2 << 20
	s, err := memory.MakeSlice[byte](a, 3*huge)
	if err != nil {
		t.Fatal(err)
	}
	if addr := uintptr(unsafe.Pointer(&s.Elems()[0])); addr%huge != 0 {
		t.Fatalf("address %#x not aligned to 2 MiB", addr)
	}
	for i := range s.Elems() {
		s.Elems()[i] = byte(i)
	}
	if s, err = memory.ResizeSlice(a, s, 5*huge); err != nil {
		t.Fatal(err)
	}
	e := s.Elems()
	if addr := uintptr(unsafe.Pointer(&e[0])); addr%huge != 0 {
		t.Fatalf("address %#x not aligned to 2 MiB after growing", addr)
	}
	for i := range 3 * huge {
		if e[i] != byte(i) {
			t.Fatalf("e[%d] = %d after growing", i, e[i])
		}
	}
	for i := 3 * huge; i < 5*huge; i++ {
		if e[i] != 0 {
			t.Fatalf("e[%d] = %d, want 0", i, e[i])
		}
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestLargeAllocationsArePageAligned(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if addr := uintptr(unsafe.Pointer(&s.Elems()[0])); addr%4096 != 0 {
		t.Fatalf("address %#x not page aligned", addr)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}
