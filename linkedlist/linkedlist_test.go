package linkedlist_test

import (
	"errors"
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

func FuzzMatchesSlice(f *testing.F) {
	f.Add(memtest.Seed(5, 1<<15))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := linkedlist.New[uint64](memtest.Local(t))
		if err != nil {
			t.Fatal(err)
		}
		defer l.Free()
		ops := memtest.NewOps(data)
		var model []uint64
		for step := 0; ops.More(); step++ {
			switch op := ops.Int(11); {
			case op < 2:
				x := ops.Uint64()
				if err := l.PushBack(x); err != nil {
					t.Fatal(err)
				}
				model = append(model, x)
			case op < 4:
				x := ops.Uint64()
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
				i := ops.Int(len(model))
				if got := l.RemoveAt(i); got != model[i] {
					t.Fatalf("step %d: RemoveAt(%d) = %d, want %d", step, i, got, model[i])
				}
				model = slices.Delete(model, i, i+1)
			case op == 7 && len(model) > 0:
				i := ops.Int(len(model))
				if got := l.At(i); got != model[i] {
					t.Fatalf("step %d: At(%d) = %d, want %d", step, i, got, model[i])
				}
				*l.Ptr(i)++
				model[i]++
			case op == 8:
				mod := uint64(ops.Int(7) + 2)
				del := func(x uint64) bool { return x%mod == 0 }
				before := len(model)
				model = slices.DeleteFunc(model, del)
				if got, want := l.DeleteFunc(del), before-len(model); got != want {
					t.Fatalf("step %d: DeleteFunc = %d, want %d", step, got, want)
				}
			case op == 9:
				if err := l.Clip(); err != nil {
					t.Fatal(err)
				}
			case op == 10 && len(model) > 0:
				x := model[ops.Int(len(model))]
				match := func(v uint64) bool { return v == x }
				if got, want := l.IndexFunc(match), slices.IndexFunc(model, match); got != want {
					t.Fatalf("step %d: IndexFunc = %d, want %d", step, got, want)
				}
			}
			if l.Len() != len(model) {
				t.Fatalf("step %d: Len = %d, want %d", step, l.Len(), len(model))
			}
		}
		if got := values(l); !slices.Equal(got, model) {
			t.Fatal("All differs from model")
		}
		if got := l.IndexFunc(func(uint64) bool { return false }); got != -1 {
			t.Fatalf("IndexFunc(no match) = %d, want -1", got)
		}
	})
}

func TestReusesFreedNodes(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	l, err := linkedlist.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Free()
	for i := range 1000 {
		if err := l.PushBack(i); err != nil {
			t.Fatal(err)
		}
	}
	for range 1000 {
		l.PopFront()
	}
	before := a.Stats()
	for i := range 1000 {
		if err := l.PushFront(i); err != nil {
			t.Fatal(err)
		}
	}
	if after := a.Stats(); after != before {
		t.Fatalf("refilling grew the pool: %+v -> %+v", before, after)
	}
	if err := l.Clip(); err != nil {
		t.Fatal(err)
	}
	if got := values(l); len(got) != 1000 || got[0] != 999 || got[999] != 0 {
		t.Fatalf("values after Clip = %v...", got[:3])
	}
}

func TestIndexOutOfRangePanics(t *testing.T) {
	t.Parallel()

	l, err := linkedlist.New[int](memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Free()
	defer func() {
		if recover() == nil {
			t.Fatal("At(0) on empty list did not panic")
		}
	}()
	l.At(0)
}
