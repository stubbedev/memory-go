package memory_test

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
)

func resident(t *testing.T, b []byte) int {
	t.Helper()
	page := unix.Getpagesize()
	start := uintptr(unsafe.Pointer(&b[0])) &^ uintptr(page-1)
	span := int(uintptr(unsafe.Pointer(&b[0]))-start) + len(b)
	vec := make([]byte, (span+page-1)/page)
	aligned := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(&b[0]), -int(uintptr(unsafe.Pointer(&b[0]))-start))), span)
	if _, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&aligned[0])), uintptr(len(aligned)), uintptr(unsafe.Pointer(&vec[0]))); errno != 0 {
		t.Fatalf("mincore: %v", errno)
	}
	n := 0
	for _, v := range vec {
		n += int(v & 1)
	}
	return n * page
}

func TestGrowingDoesNotCommitNewBytes(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.MakeSlice[byte](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	const n = 64 << 20
	if s, err = memory.ResizeSlice(a, s, n); err != nil {
		t.Fatal(err)
	}
	if got := resident(t, s.Elems()); got > 2<<20 {
		t.Fatalf("growing committed %d of %d bytes", got, n)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestReusingDecommittedMappingStaysUncommitted(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const n = 64 << 20
	s, err := memory.MakeSlice[byte](a, n)
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Elems() {
		s.Elems()[i] = 0xff
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
	if s, err = memory.MakeSlice[byte](a, n); err != nil {
		t.Fatal(err)
	}
	if got := resident(t, s.Elems()); got > 1<<20 {
		t.Fatalf("reuse committed %d of %d bytes", got, n)
	}
	for i, v := range s.Elems() {
		if v != 0 {
			t.Fatalf("s[%d] = %d, want 0", i, v)
		}
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestLargeGrowthRemapsInsteadOfCopying(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const n = 64 << 20
	s, err := memory.MakeSlice[byte](a, n)
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Elems() {
		s.Elems()[i] = byte(i)
	}
	if s, err = memory.ResizeSlice(a, s, 2*n); err != nil {
		t.Fatal(err)
	}
	if got := a.Footprint().Retained; got != 0 {
		t.Fatalf("growing retired %d bytes, want the mapping remapped", got)
	}
	e := s.Elems()
	if got := resident(t, e); got > n+1<<20 {
		t.Fatalf("resident %d after growing %d, want no second copy", got, n)
	}
	for i := range n {
		if e[i] != byte(i) {
			t.Fatalf("e[%d] = %d after remap", i, e[i])
		}
	}
	for i := n; i < 2*n; i++ {
		if e[i] != 0 {
			t.Fatalf("e[%d] = %d, want 0", i, e[i])
		}
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestPopulateMakesEveryPageResident(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const n = 16 << 20
	s, err := memory.MakeSlice[byte](a, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Populate(s); err != nil {
		t.Fatal(err)
	}
	if got := resident(t, s.Elems()); got < n {
		t.Fatalf("resident %d of %d", got, n)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}
