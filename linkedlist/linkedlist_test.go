package linkedlist_test

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/linkedlist"
)

func values[T any](l *linkedlist.List[T]) []T {
	var out []T
	for _, v := range l.All() {
		out = append(out, v)
	}
	return out
}

func TestRejectsPointerElements(t *testing.T) {
	t.Parallel()

	if _, err := linkedlist.New[[]int](memtest.Local(t)); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("err = %v, want ErrHoldsPointers", err)
	}
}

func TestMatchesSlice(t *testing.T) {
	t.Parallel()

	l, err := linkedlist.New[int32](memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(5, 6))
	var model []int32
	for step := range 20_000 {
		switch op := rng.IntN(9); {
		case op < 2:
			x := rng.Int32()
			if err := l.PushBack(x); err != nil {
				t.Fatal(err)
			}
			model = append(model, x)
		case op < 4:
			x := rng.Int32()
			if err := l.PushFront(x); err != nil {
				t.Fatal(err)
			}
			model = slices.Insert(model, 0, x)
		case op == 4:
			got, ok := l.PopFront()
			if ok != (len(model) > 0) || ok && got != model[0] {
				t.Fatalf("step %d: PopFront = %d, %v", step, got, ok)
			}
			if ok {
				model = model[1:]
			}
		case op == 5:
			got, ok := l.PopBack()
			if ok != (len(model) > 0) || ok && got != model[len(model)-1] {
				t.Fatalf("step %d: PopBack = %d, %v", step, got, ok)
			}
			if ok {
				model = model[:len(model)-1]
			}
		case op == 6 && len(model) > 0:
			i := rng.IntN(len(model))
			if got := l.RemoveAt(i); got != model[i] {
				t.Fatalf("step %d: RemoveAt(%d) = %d, want %d", step, i, got, model[i])
			}
			model = slices.Delete(model, i, i+1)
		case op == 7 && len(model) > 0:
			i := rng.IntN(len(model))
			if got := l.At(i); got != model[i] {
				t.Fatalf("step %d: At(%d) = %d, want %d", step, i, got, model[i])
			}
			*l.Ptr(i)++
			model[i]++
		case op == 8 && rng.IntN(100) == 0:
			even := func(x int32) bool { return x%2 == 0 }
			before := len(model)
			model = slices.DeleteFunc(model, even)
			if got, want := l.DeleteFunc(even), before-len(model); got != want {
				t.Fatalf("step %d: DeleteFunc = %d, want %d", step, got, want)
			}
		}
		if l.Len() != len(model) {
			t.Fatalf("step %d: Len = %d, want %d", step, l.Len(), len(model))
		}
	}
	if got := values(l); !slices.Equal(got, model) {
		t.Fatal("All differs from model")
	}
	if len(model) > 0 {
		want := len(model) / 2
		if got := l.IndexFunc(func(x int32) bool { return x == model[want] }); got != slices.Index(model, model[want]) {
			t.Fatalf("IndexFunc = %d, want %d", got, slices.Index(model, model[want]))
		}
	}
	if got := l.IndexFunc(func(int32) bool { return false }); got != -1 {
		t.Fatalf("IndexFunc(no match) = %d, want -1", got)
	}
	if err := l.Free(); err != nil {
		t.Fatal(err)
	}
	if l.Len() != 0 {
		t.Fatalf("Len after Free = %d", l.Len())
	}
}

func TestReusesFreedNodes(t *testing.T) {
	t.Parallel()

	l, err := linkedlist.New[int](memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	for round := range 3 {
		for i := range 1000 {
			if err := l.PushBack(round*1000 + i); err != nil {
				t.Fatal(err)
			}
		}
		for range 1000 {
			l.PopFront()
		}
	}
	if err := l.PushFront(1); err != nil {
		t.Fatal(err)
	}
	if err := l.PushBack(2); err != nil {
		t.Fatal(err)
	}
	if got := values(l); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("values = %v, want [1 2]", got)
	}
}

func TestIndexOutOfRangePanics(t *testing.T) {
	t.Parallel()

	l, err := linkedlist.New[int](memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("At(0) on empty list did not panic")
		}
	}()
	l.At(0)
}
