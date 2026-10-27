package vector_test

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/vector"
)

func TestRejectsPointerElements(t *testing.T) {
	t.Parallel()

	if _, err := vector.New[string](memtest.Local(t), 0); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("err = %v, want ErrHoldsPointers", err)
	}
}

func TestMatchesSlice(t *testing.T) {
	t.Parallel()

	v, err := vector.New[int](memtest.Local(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	var model []int
	for step := range 20_000 {
		switch op := rng.IntN(10); {
		case op < 5:
			x := rng.Int()
			if err := v.Push(x); err != nil {
				t.Fatal(err)
			}
			model = append(model, x)
		case op == 5:
			got, ok := v.Pop()
			if ok != (len(model) > 0) {
				t.Fatalf("step %d: Pop ok = %v with len %d", step, ok, len(model))
			}
			if ok {
				if want := model[len(model)-1]; got != want {
					t.Fatalf("step %d: Pop = %d, want %d", step, got, want)
				}
				model = model[:len(model)-1]
			}
		case op == 6 && len(model) > 0:
			i := rng.IntN(len(model))
			if got := v.RemoveAt(i); got != model[i] {
				t.Fatalf("step %d: RemoveAt(%d) = %d, want %d", step, i, got, model[i])
			}
			model = slices.Delete(model, i, i+1)
		case op == 7 && len(model) > 0:
			i, x := rng.IntN(len(model)), rng.Int()
			v.Set(i, x)
			model[i] = x
		case op == 8 && rng.IntN(50) == 0:
			odd := func(x int) bool { return x%2 != 0 }
			before := len(model)
			model = slices.DeleteFunc(model, odd)
			if got, want := v.DeleteFunc(odd), before-len(model); got != want {
				t.Fatalf("step %d: DeleteFunc = %d, want %d", step, got, want)
			}
		case op == 9 && len(model) > 0 && rng.IntN(20) == 0:
			n := rng.IntN(len(model))
			v.Truncate(n)
			model = model[:n]
		}
		if v.Len() != len(model) {
			t.Fatalf("step %d: Len = %d, want %d", step, v.Len(), len(model))
		}
	}
	if !slices.Equal(v.Slice(), model) {
		t.Fatal("Slice differs from model")
	}
	var walked []int
	for i, x := range v.All() {
		if i != len(walked) {
			t.Fatalf("All index = %d, want %d", i, len(walked))
		}
		walked = append(walked, x)
	}
	if !slices.Equal(walked, model) {
		t.Fatal("All differs from model")
	}
	if err := v.Free(); err != nil {
		t.Fatal(err)
	}
	if v.Len() != 0 || v.Cap() != 0 {
		t.Fatalf("after Free Len, Cap = %d, %d", v.Len(), v.Cap())
	}
}

func TestIndexOutOfRangePanics(t *testing.T) {
	t.Parallel()

	v, err := vector.New[int](memtest.Local(t), 8)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("At(0) on empty vector did not panic")
		}
	}()
	v.At(0)
}
