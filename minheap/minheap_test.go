package minheap_test

import (
	"cmp"
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/minheap"
)

func less(a, b int) bool { return a < b }

func drain(t *testing.T, h *minheap.Heap[int]) []int {
	t.Helper()
	var out []int
	for {
		v, ok := h.Pop()
		if !ok {
			return out
		}
		out = append(out, v)
	}
}

func TestRejectsPointerElements(t *testing.T) {
	t.Parallel()

	if _, err := minheap.New(memtest.Local(t), func(a, b *int) bool { return *a < *b }); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("err = %v, want ErrHoldsPointers", err)
	}
}

func TestPopsInOrder(t *testing.T) {
	t.Parallel()

	h, err := minheap.New(memtest.Local(t), less)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Peek(); ok {
		t.Fatal("Peek on empty heap reported a value")
	}
	rng := rand.New(rand.NewPCG(7, 8))
	model := make([]int, 5000)
	for i := range model {
		model[i] = rng.IntN(1000)
		if err := h.Push(model[i]); err != nil {
			t.Fatal(err)
		}
	}
	if top, _ := h.Peek(); top != slices.Min(model) {
		t.Fatalf("Peek = %d, want %d", top, slices.Min(model))
	}
	slices.Sort(model)
	if got := drain(t, h); !slices.Equal(got, model) {
		t.Fatal("pop order differs from sorted input")
	}
	if h.Len() != 0 {
		t.Fatalf("Len after drain = %d", h.Len())
	}
}

func TestDeleteFuncKeepsHeapOrder(t *testing.T) {
	t.Parallel()

	h, err := minheap.New(memtest.Local(t), func(a, b int) bool { return cmp.Less(b, a) })
	if err != nil {
		t.Fatal(err)
	}
	for i := range 200 {
		if err := h.Push(i * 7 % 200); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.DeleteFunc(func(x int) bool { return x%3 == 0 }); got != 67 {
		t.Fatalf("DeleteFunc removed %d, want 67", got)
	}
	var want []int
	for i := 199; i >= 0; i-- {
		if i%3 != 0 {
			want = append(want, i)
		}
	}
	if got := drain(t, h); !slices.Equal(got, want) {
		t.Fatalf("pop order after DeleteFunc = %v, want %v", got, want)
	}
	if err := h.Free(); err != nil {
		t.Fatal(err)
	}
}
