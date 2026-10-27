package typedarena_test

import (
	"errors"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/typedarena"
)

type particle struct {
	X, Y, VX, VY float32
}

func TestRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := typedarena.New[particle](a, 0); !errors.Is(err, typedarena.ErrChunkSize) {
		t.Errorf("chunk 0: err = %v, want ErrChunkSize", err)
	}
	if _, err := typedarena.New[*particle](a, 8); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Errorf("pointer type: err = %v, want ErrHoldsPointers", err)
	}
	ta, err := typedarena.New[particle](a, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ta.AllocSlice(-1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Errorf("AllocSlice(-1): err = %v, want ErrNegativeLen", err)
	}
}

func TestAllocationsAreDistinctAndStable(t *testing.T) {
	t.Parallel()

	ta, err := typedarena.New[particle](memtest.Local(t), 16)
	if err != nil {
		t.Fatal(err)
	}
	var ps []*particle
	for i := range 1000 {
		p, err := ta.Alloc()
		if err != nil {
			t.Fatal(err)
		}
		if *p != (particle{}) {
			t.Fatalf("Alloc %d not zeroed: %+v", i, *p)
		}
		p.X = float32(i)
		ps = append(ps, p)
	}
	big, err := ta.AllocSlice(100)
	if err != nil {
		t.Fatal(err)
	}
	small, err := ta.AllocSlice(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(big) != 100 || cap(big) != 100 || len(small) != 10 || cap(small) != 10 {
		t.Fatalf("len/cap = %d/%d, %d/%d", len(big), cap(big), len(small), cap(small))
	}
	for i := range big {
		big[i].Y = -1
	}
	for i := range small {
		small[i].Y = -2
	}
	seen := make(map[*particle]bool, len(ps))
	for i, p := range ps {
		if p.X != float32(i) || p.Y != 0 {
			t.Fatalf("particle %d = %+v, overwritten", i, *p)
		}
		if seen[p] {
			t.Fatalf("particle %d shares an address", i)
		}
		seen[p] = true
	}
	if err := ta.Free(); err != nil {
		t.Fatal(err)
	}
}
