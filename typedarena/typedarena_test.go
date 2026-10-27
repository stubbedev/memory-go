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
	Next         memory.Ref[particle]
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
	defer ta.Free()
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
	defer ta.Free()
	var head memory.Ref[particle]
	for i := range 1000 {
		r, err := ta.Alloc()
		if err != nil {
			t.Fatal(err)
		}
		p := r.Value()
		if *p != (particle{}) {
			t.Fatalf("Alloc %d not zeroed: %+v", i, *p)
		}
		p.X, p.Next = float32(i), head
		head = r
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
	want := float32(999)
	for r := head; !r.IsNil(); r = r.Value().Next {
		if p := r.Value(); p.X != want || p.Y != 0 {
			t.Fatalf("particle %v = %+v, overwritten", want, *p)
		}
		want--
	}
	if want != -1 {
		t.Fatalf("walked to %v, want all 1000 particles", want+1)
	}
}
