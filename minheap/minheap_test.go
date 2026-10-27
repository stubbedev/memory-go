package minheap_test

import (
	"errors"
	"slices"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/minheap"
)

func TestRejectsPointerElements(t *testing.T) {
	t.Parallel()

	if _, err := minheap.New(memtest.Local(t), func(a, b *int) bool { return *a < *b }); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("err = %v, want ErrHoldsPointers", err)
	}
}

func FuzzMatchesSortedSlice(f *testing.F) {
	f.Add(memtest.Seed(7, 1<<15))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		h, err := minheap.New(memtest.Local(t), func(a, b uint16) bool { return a < b })
		if err != nil {
			t.Fatal(err)
		}
		defer h.Free()
		ops := memtest.NewOps(data)
		var model []uint16
		for step := 0; ops.More(); step++ {
			switch op := ops.Int(8); {
			case op < 4:
				x := uint16(ops.Int(1 << 16))
				if err := h.Push(x); err != nil {
					t.Fatal(err)
				}
				i, _ := slices.BinarySearch(model, x)
				model = slices.Insert(model, i, x)
			case op < 6:
				got, ok := h.Pop()
				if ok != (len(model) > 0) || ok && got != model[0] {
					t.Fatalf("step %d: Pop = %d, %v; model min %v", step, got, ok, model[:min(1, len(model))])
				}
				if ok {
					model = model[1:]
				}
			case op == 6:
				mod := uint16(ops.Int(7) + 2)
				del := func(x uint16) bool { return x%mod == 0 }
				before := len(model)
				model = slices.DeleteFunc(model, del)
				if got, want := h.DeleteFunc(del), before-len(model); got != want {
					t.Fatalf("step %d: DeleteFunc = %d, want %d", step, got, want)
				}
			case op == 7:
				if err := h.Clip(); err != nil {
					t.Fatal(err)
				}
			}
			top, ok := h.Peek()
			if h.Len() != len(model) || ok != (len(model) > 0) || ok && top != model[0] {
				t.Fatalf("step %d: Len, Peek = %d, %d, %v; want %d, %v", step, h.Len(), top, ok, len(model), model[:min(1, len(model))])
			}
		}
		var all []uint16
		for _, x := range h.All() {
			all = append(all, x)
		}
		slices.Sort(all)
		if !slices.Equal(all, model) {
			t.Fatal("All differs from model")
		}
	})
}
