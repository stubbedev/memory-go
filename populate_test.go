package memory_test

import (
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func TestPopulateKeepsContents(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	for _, n := range []int{0, 1, 100, 8192, 1 << 20, 8 << 20} {
		s, err := memory.MakeSlice[byte](a, n)
		if err != nil {
			t.Fatal(err)
		}
		for i := range s.Elems() {
			s.Elems()[i] = byte(i)
		}
		if err := memory.Populate(s); err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		for i, v := range s.Elems() {
			if v != byte(i) {
				t.Fatalf("n=%d: s[%d] = %d", n, i, v)
			}
		}
		if err := memory.FreeSlice(a, s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegionPopulate(t *testing.T) {
	t.Parallel()

	r, err := memory.NewRegion[uint64](1 << 24)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Populate(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Grow(1 << 20); err != nil {
		t.Fatal(err)
	}
	if err := r.Populate(); err != nil {
		t.Fatal(err)
	}
	for i, v := range r.Elems() {
		if v != 0 {
			t.Fatalf("element %d = %d", i, v)
		}
	}
}
