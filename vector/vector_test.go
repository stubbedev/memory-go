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

func TestStableVectorKeepsPointersValid(t *testing.T) {
	t.Parallel()

	v, err := vector.NewStable[uint64](1 << 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := v.Free(); err != nil {
			t.Error(err)
		}
	})
	if err := v.Push(7); err != nil {
		t.Fatal(err)
	}
	first := v.Ptr(0)
	for i := range 1 << 22 {
		if err := v.Push(uint64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if first != v.Ptr(0) || *first != 7 {
		t.Fatal("growing moved the first element")
	}
	if got := v.At(v.Len() - 1); got != 1<<22-1 {
		t.Fatalf("last = %d", got)
	}
	v.Truncate(10)
	if err := v.Clip(); err != nil {
		t.Fatal(err)
	}
	if v.Cap() != 10 || first != v.Ptr(0) {
		t.Fatalf("Clip: cap %d", v.Cap())
	}
	if err := v.Push(1); err != nil {
		t.Fatal(err)
	}
	if got := v.At(10); got != 1 {
		t.Fatalf("v[10] = %d", got)
	}
}

func TestStableVectorStopsAtItsLimit(t *testing.T) {
	t.Parallel()

	v, err := vector.NewStable[uint64](5)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	for i := range 5 {
		if err := v.Push(uint64(i)); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	if err := v.Push(5); !errors.Is(err, memory.ErrRegionFull) {
		t.Fatalf("err = %v, want ErrRegionFull", err)
	}
}

func TestAppendMatchesPush(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	v, err := vector.New[int](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	want := make([]int, 0, 1000)
	for i := range 100 {
		chunk := make([]int, i)
		for j := range chunk {
			chunk[j] = i*100 + j
		}
		if err := v.Append(chunk...); err != nil {
			t.Fatal(err)
		}
		want = append(want, chunk...)
	}
	if !slices.Equal(v.Slice(), want) {
		t.Fatal("Append differs from a Go slice")
	}
	if err := v.Append(); err != nil {
		t.Fatal(err)
	}
	if v.Len() != len(want) {
		t.Fatalf("Len = %d", v.Len())
	}
}

func BenchmarkAppend(b *testing.B) {
	a := memtest.Local(b)
	v, err := vector.New[uint64](a, 1<<16)
	if err != nil {
		b.Fatal(err)
	}
	defer v.Free()
	src := make([]uint64, 1<<16)
	for b.Loop() {
		v.Truncate(0)
		if err := v.Append(src...); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(1 << 19)
}

func TestStableAppendPastLimitFails(t *testing.T) {
	t.Parallel()

	v, err := vector.NewStable[int](5)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	if err := v.Append(1, 2, 3, 4, 5, 6, 7, 8, 9, 10); !errors.Is(err, memory.ErrRegionFull) {
		t.Fatalf("Append past the limit: %v", err)
	}
	if v.Len() != 0 {
		t.Fatalf("Len = %d after failed Append", v.Len())
	}
	if err := v.Append(1, 2, 3, 4, 5); err != nil {
		t.Fatal(err)
	}
	if got := v.Slice(); len(got) != 5 || got[4] != 5 {
		t.Fatalf("Slice = %v", got)
	}
}

func TestStableReservePastLimitFails(t *testing.T) {
	t.Parallel()

	v, err := vector.NewStable[int](5)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	if err := v.Reserve(100); !errors.Is(err, memory.ErrRegionFull) {
		t.Fatalf("Reserve past the limit: %v", err)
	}
	if v.Cap() != 0 {
		t.Fatalf("Cap = %d after failed Reserve", v.Cap())
	}
}

func TestStablePushFillsToTheLimit(t *testing.T) {
	t.Parallel()

	v, err := vector.NewStable[int](5)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	for i := range 5 {
		if err := v.Push(i); err != nil {
			t.Fatalf("Push(%d): %v", i, err)
		}
	}
	if err := v.Push(5); !errors.Is(err, memory.ErrRegionFull) {
		t.Fatalf("Push past the limit: %v", err)
	}
	if v.Len() != 5 || v.At(4) != 4 {
		t.Fatalf("Len = %d", v.Len())
	}
}

func TestResizeWhoseOldBufferFailsToFreeKeepsTheNewOne(t *testing.T) {
	t.Parallel()

	l := memtest.Local(t)
	f := memory.NewFaulty(l)
	v, err := vector.New[int](f, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Append(1, 2, 3, 4); err != nil {
		t.Fatal(err)
	}
	f.FailFreesAfter(0)
	if err := v.Reserve(1 << 12); !errors.Is(err, memory.ErrInjected) {
		t.Fatalf("Reserve: %v", err)
	}
	f.FailFreesAfter(-1)
	if v.Cap() < 1<<12 {
		t.Fatalf("Cap = %d", v.Cap())
	}
	if got := v.Slice(); len(got) != 4 || got[3] != 4 {
		t.Fatalf("Slice = %v", got)
	}
	if err := v.Push(5); err != nil {
		t.Fatal(err)
	}
	if err := v.Free(); err != nil {
		t.Fatal(err)
	}
}

func TestFromCopies(t *testing.T) {
	t.Parallel()

	values := []int{1, 2, 3}
	v, err := vector.From(memtest.Local(t), values)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	values[0] = 9
	if v.Len() != 3 || v.At(0) != 1 || v.At(2) != 3 {
		t.Fatalf("Slice = %v", v.Slice())
	}
}
