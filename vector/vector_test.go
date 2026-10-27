package vector_test

import (
	"errors"
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

func FuzzMatchesSlice(f *testing.F) {
	f.Add(memtest.Seed(1, 1<<15))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := vector.New[uint64](memtest.Local(t), 0)
		if err != nil {
			t.Fatal(err)
		}
		defer v.Free()
		ops := memtest.NewOps(data)
		var model []uint64
		for step := 0; ops.More(); step++ {
			switch op := ops.Int(12); {
			case op < 5:
				x := ops.Uint64()
				if err := v.Push(x); err != nil {
					t.Fatal(err)
				}
				model = append(model, x)
			case op == 5:
				got, ok := v.Pop()
				if ok != (len(model) > 0) || ok && got != model[len(model)-1] {
					t.Fatalf("step %d: Pop = %d, %v", step, got, ok)
				}
				if ok {
					model = model[:len(model)-1]
				}
			case op == 6 && len(model) > 0:
				i := ops.Int(len(model))
				if got := v.RemoveAt(i); got != model[i] {
					t.Fatalf("step %d: RemoveAt(%d) = %d, want %d", step, i, got, model[i])
				}
				model = slices.Delete(model, i, i+1)
			case op == 7 && len(model) > 0:
				i, x := ops.Int(len(model)), ops.Uint64()
				v.Set(i, x)
				model[i] = x
			case op == 8:
				mod := uint64(ops.Int(7) + 2)
				del := func(x uint64) bool { return x%mod == 0 }
				before := len(model)
				model = slices.DeleteFunc(model, del)
				if got, want := v.DeleteFunc(del), before-len(model); got != want {
					t.Fatalf("step %d: DeleteFunc = %d, want %d", step, got, want)
				}
			case op == 9 && len(model) > 0:
				n := ops.Int(len(model))
				v.Truncate(n)
				model = model[:n]
			case op == 10:
				if err := v.Clip(); err != nil {
					t.Fatal(err)
				}
				if v.Cap() != len(model) {
					t.Fatalf("step %d: Cap after Clip = %d, want %d", step, v.Cap(), len(model))
				}
			case op == 11:
				n := len(model) + ops.Int(64)
				if err := v.Reserve(n); err != nil {
					t.Fatal(err)
				}
				if v.Cap() < n {
					t.Fatalf("step %d: Cap after Reserve(%d) = %d", step, n, v.Cap())
				}
			}
			if v.Len() != len(model) {
				t.Fatalf("step %d: Len = %d, want %d", step, v.Len(), len(model))
			}
		}
		if !slices.Equal(v.Slice(), model) {
			t.Fatal("Slice differs from model")
		}
		var walked []uint64
		for i, x := range v.All() {
			if i != len(walked) {
				t.Fatalf("All index = %d, want %d", i, len(walked))
			}
			walked = append(walked, x)
		}
		if !slices.Equal(walked, model) {
			t.Fatal("All differs from model")
		}
	})
}

func TestIndexOutOfRangePanics(t *testing.T) {
	t.Parallel()

	v, err := vector.New[int](memtest.Local(t), 8)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	defer func() {
		if recover() == nil {
			t.Fatal("At(0) on empty vector did not panic")
		}
	}()
	v.At(0)
}
