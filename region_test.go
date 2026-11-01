package memory_test

import (
	"errors"
	"testing"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
)

type regionNode struct {
	Key         uint64
	Left, Right memory.Index[regionNode]
}

func TestRegionHandlesAreFourBytes(t *testing.T) {
	t.Parallel()

	if got := unsafe.Sizeof(regionNode{}); got != 16 {
		t.Fatalf("node with two handles is %d bytes, want 16", got)
	}
}

func TestRegionGrowsWithoutMoving(t *testing.T) {
	t.Parallel()

	r, err := memory.NewRegion[regionNode](1 << 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	first, err := r.Grow(1)
	if err != nil {
		t.Fatal(err)
	}
	if first.IsNil() {
		t.Fatal("first Index is nil")
	}
	root := r.Ptr(first)
	root.Key = 7
	var last memory.Index[regionNode]
	for i := range 3 << 20 {
		idx, err := r.Grow(1)
		if err != nil {
			t.Fatal(err)
		}
		p := r.Ptr(idx)
		if *p != (regionNode{}) {
			t.Fatalf("element %d not zeroed: %+v", i, *p)
		}
		p.Key, p.Left = uint64(i), last
		last = idx
	}
	if r.Ptr(first) != root || root.Key != 7 {
		t.Fatal("growing moved the first element")
	}
	if got := r.Ptr(r.Ptr(last).Left).Key; got != 3<<20-2 {
		t.Fatalf("linked key = %d", got)
	}
	if got := r.Len(); got != 3<<20+1 {
		t.Fatalf("Len = %d", got)
	}
	if foot := r.Footprint(); foot.Committed < 16*r.Len() || foot.Committed > 16*r.Len()+2<<20 {
		t.Fatalf("Footprint = %+v for %d elements", foot, r.Len())
	}
}

func TestRegionTruncateZeroes(t *testing.T) {
	t.Parallel()

	for _, n := range []int{100, 2 << 20} {
		r, err := memory.NewRegion[uint64](1 << 24)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Grow(n); err != nil {
			t.Fatal(err)
		}
		for i := range r.Elems() {
			r.Elems()[i] = ^uint64(0)
		}
		if err := r.Truncate(1); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Grow(n - 1); err != nil {
			t.Fatal(err)
		}
		for i, v := range r.Elems()[1:] {
			if v != 0 {
				t.Fatalf("n=%d: element %d = %x after regrowth", n, i+1, v)
			}
		}
		if r.Elems()[0] != ^uint64(0) {
			t.Fatal("Truncate lost a kept element")
		}
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		if err := r.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
	}
}

func TestRegionLimits(t *testing.T) {
	t.Parallel()

	if _, err := memory.NewRegion[string](4); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("pointer type: err = %v", err)
	}
	if _, err := memory.NewRegion[int](-1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Fatalf("negative: err = %v", err)
	}
	r, err := memory.NewRegion[int](2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Grow(3); !errors.Is(err, memory.ErrRegionFull) {
		t.Fatalf("overgrow: err = %v", err)
	}
	if _, err := r.Grow(2); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("nil Index did not panic")
		}
	}()
	r.Ptr(0)
}
