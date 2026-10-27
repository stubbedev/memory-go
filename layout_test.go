package memory_test

import (
	"errors"
	"math"
	"sync"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

type point struct {
	X, Y int64
	Tags [2]uint8
}

type withName struct {
	ID   int
	Name string
}

type nested struct {
	Inner [3]struct {
		Buf []byte
	}
}

func TestPointerTypesRejected(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	tests := []struct {
		name string
		new  func() error
		want string
	}{
		{name: "string", new: func() error { _, err := memory.New[string](a); return err }, want: "memory: string: type holds Go pointers"},
		{name: "pointer", new: func() error { _, err := memory.New[*int](a); return err }, want: "memory: *int: type holds Go pointers"},
		{name: "interface", new: func() error { _, err := memory.New[any](a); return err }, want: "memory: interface {}: type holds Go pointers"},
		{name: "map", new: func() error { _, err := memory.New[map[int]int](a); return err }, want: "memory: map[int]int: type holds Go pointers"},
		{name: "struct field", new: func() error { _, err := memory.New[withName](a); return err }, want: "memory: memory_test.withName: .Name is string: type holds Go pointers"},
		{name: "nested", new: func() error { _, err := memory.MakeSlice[nested](a, 3); return err }, want: "memory: memory_test.nested: .Inner[i].Buf is []uint8: type holds Go pointers"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.new()
			if !errors.Is(err, memory.ErrHoldsPointers) {
				t.Fatalf("err = %v, want ErrHoldsPointers", err)
			}
			if err.Error() != tc.want {
				t.Errorf("err = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestPointerFreeTypesAccepted(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := memory.New[point](a); err != nil {
		t.Errorf("point: %v", err)
	}
	if _, err := memory.New[[0]*int](a); err != nil {
		t.Errorf("[0]*int: %v", err)
	}
	if _, err := memory.New[complex128](a); err != nil {
		t.Errorf("complex128: %v", err)
	}
}

func TestNewIsZeroedAndWritable(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	if *p != (point{}) {
		t.Fatalf("*p = %+v, want zero", *p)
	}
	*p = point{X: 1, Y: -2, Tags: [2]uint8{3, 4}}
	if p.X != 1 || p.Y != -2 || p.Tags[1] != 4 {
		t.Fatalf("*p = %+v after write", *p)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	if err := memory.Free[point](a, nil); err != nil {
		t.Fatalf("Free(nil) = %v", err)
	}
}

func TestZeroSizeType(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := memory.New[struct{}](a)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("New[struct{}] returned nil")
	}
	s, err := memory.MakeSlice[struct{}](a, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 5 {
		t.Fatalf("len = %d, want 5", len(s))
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestMakeSliceLengths(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.MakeSlice[int32](a, 0)
	if err != nil || s != nil {
		t.Fatalf("MakeSlice(0) = %v, %v; want nil, nil", s, err)
	}
	if _, err := memory.MakeSlice[int32](a, -1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Fatalf("MakeSlice(-1) err = %v, want ErrNegativeLen", err)
	}
	if _, err := memory.MakeSlice[int64](a, math.MaxInt/4); !errors.Is(err, memory.ErrTooLarge) {
		t.Fatalf("MakeSlice(huge) err = %v, want ErrTooLarge", err)
	}
	s, err = memory.MakeSlice[int32](a, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 1000 || cap(s) != 1000 {
		t.Fatalf("len, cap = %d, %d; want 1000, 1000", len(s), cap(s))
	}
	for i, v := range s {
		if v != 0 {
			t.Fatalf("s[%d] = %d, want 0", i, v)
		}
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestResizeSlice(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.ResizeSlice[int](a, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	copy(s, []int{1, 2, 3})
	s, err = memory.ResizeSlice(a, s, 2)
	if err != nil {
		t.Fatal(err)
	}
	s, err = memory.ResizeSlice(a, s, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if s[0] != 1 || s[1] != 2 {
		t.Fatalf("prefix = %v, want [1 2]", s[:2])
	}
	for i, v := range s[2:] {
		if v != 0 {
			t.Fatalf("s[%d] = %d after grow, want 0", i+2, v)
		}
	}
	s, err = memory.ResizeSlice(a, s, 0)
	if err != nil || s != nil {
		t.Fatalf("ResizeSlice(0) = %v, %v; want nil, nil", s, err)
	}
}

func TestSharedConcurrentUse(t *testing.T) {
	t.Parallel()

	a := &memory.Shared{}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 1000 {
				p, err := memory.New[point](a)
				if err != nil {
					t.Error(err)
					return
				}
				*p = point{X: int64(g), Y: int64(i)}
				if p.X != int64(g) || p.Y != int64(i) {
					t.Errorf("p = %+v, want {%d %d}", *p, g, i)
				}
				if err := memory.Free(a, p); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if err := a.Trim(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkNewFree(b *testing.B) {
	a := memtest.Local(b)
	for b.Loop() {
		p, err := memory.New[point](a)
		if err != nil {
			b.Fatal(err)
		}
		if err := memory.Free(a, p); err != nil {
			b.Fatal(err)
		}
	}
}

var sink *point

func BenchmarkGoNew(b *testing.B) {
	for b.Loop() {
		sink = new(point)
	}
}
