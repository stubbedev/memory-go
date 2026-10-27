package memory_test

import (
	"errors"
	"math"
	"strings"
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

type treeNode struct {
	Key         int
	Left, Right memory.Ptr[treeNode]
	Parent      memory.Ref[treeNode]
	Label       memory.String
	Children    memory.Slice[memory.Ref[treeNode]]
}

type smuggler struct {
	memory.Ptr[int]
	Name string
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
		{name: "embedded handle", new: func() error { _, err := memory.New[smuggler](a); return err }, want: "memory: memory_test.smuggler: .Name is string: type holds Go pointers"},
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
	check := func(name string, err error) {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	p1, err := memory.New[point](a)
	check("point", err)
	p2, err := memory.New[[0]*int](a)
	check("[0]*int", err)
	p3, err := memory.New[treeNode](a)
	check("treeNode with handle fields", err)
	check("free", errors.Join(memory.Free(a, p1), memory.Free(a, p2), memory.Free(a, p3)))
}

func TestNewIsZeroedAndWritable(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	if *p.Value() != (point{}) {
		t.Fatalf("*p = %+v, want zero", *p.Value())
	}
	*p.Value() = point{X: 1, Y: -2, Tags: [2]uint8{3, 4}}
	if v := p.Ref().Value(); v.X != 1 || v.Y != -2 || v.Tags[1] != 4 {
		t.Fatalf("*p = %+v after write", *v)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
	if err := memory.Free(a, memory.Ptr[point]{}); err != nil {
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
	if p.IsNil() {
		t.Fatal("New[struct{}] returned nil")
	}
	s, err := memory.MakeSlice[struct{}](a, 5)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 5 || len(s.Elems()) != 5 {
		t.Fatalf("Len = %d, want 5", s.Len())
	}
	if err := errors.Join(memory.Free(a, p), memory.FreeSlice(a, s)); err != nil {
		t.Fatal(err)
	}
}

func TestMakeSliceLengths(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.MakeSlice[int32](a, 0)
	if err != nil || s.Len() != 0 || s.Elems() != nil {
		t.Fatalf("MakeSlice(0) = %v, %v; want empty, nil", s.Elems(), err)
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
	e := s.Elems()
	if len(e) != 1000 || cap(e) != 1000 {
		t.Fatalf("len, cap = %d, %d; want 1000, 1000", len(e), cap(e))
	}
	for i, v := range e {
		if v != 0 {
			t.Fatalf("s[%d] = %d, want 0", i, v)
		}
	}
	e[999] = 7
	if *s.Ref(999).Value() != 7 {
		t.Fatal("Ref(999) does not alias the element")
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func TestResizeSlice(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.ResizeSlice(a, memory.Slice[int]{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	copy(s.Elems(), []int{1, 2, 3})
	if s, err = memory.ResizeSlice(a, s, 2); err != nil {
		t.Fatal(err)
	}
	if s, err = memory.ResizeSlice(a, s, 100_000); err != nil {
		t.Fatal(err)
	}
	e := s.Elems()
	if e[0] != 1 || e[1] != 2 {
		t.Fatalf("prefix = %v, want [1 2]", e[:2])
	}
	for i, v := range e[2:] {
		if v != 0 {
			t.Fatalf("s[%d] = %d after grow, want 0", i+2, v)
		}
	}
	kept, err := memory.ResizeSlice(a, s, -1)
	if !errors.Is(err, memory.ErrNegativeLen) || kept != s {
		t.Fatalf("ResizeSlice(-1) = %v, %v; want s unchanged, ErrNegativeLen", kept.Len(), err)
	}
	s, err = memory.ResizeSlice(a, s, 0)
	if err != nil || s.Len() != 0 {
		t.Fatalf("ResizeSlice(0) = %d, %v; want empty, nil", s.Len(), err)
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	s, err := memory.NewString(a, "héllo")
	if err != nil {
		t.Fatal(err)
	}
	if s.String() != "héllo" || s.Len() != len("héllo") {
		t.Fatalf("String = %q, Len = %d", s.String(), s.Len())
	}
	var want strings.Builder
	want.WriteString("héllo")
	for i := range 2000 {
		part := strings.Repeat("x", i%7)
		if s, err = memory.AppendString(a, s, part); err != nil {
			t.Fatal(err)
		}
		want.WriteString(part)
	}
	if s.String() != want.String() {
		t.Fatal("appended String differs from strings.Builder")
	}
	s.Bytes()[0] = 'H'
	if !strings.HasPrefix(s.String(), "Héllo") {
		t.Fatal("Bytes does not alias String")
	}
	if err := memory.FreeString(a, s); err != nil {
		t.Fatal(err)
	}
	var empty memory.String
	if empty.String() != "" || empty.Len() != 0 {
		t.Fatal("zero String is not empty")
	}
	if err := memory.FreeString(a, empty); err != nil {
		t.Fatal(err)
	}
}

func TestStats(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if s := a.Stats(); s != (memory.Stats{}) {
		t.Fatalf("fresh Stats = %+v", s)
	}
	p, err := memory.New[point](a)
	if err != nil {
		t.Fatal(err)
	}
	s, err := memory.MakeSlice[byte](a, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got.Live != 2 || got.Bytes < 100+24 {
		t.Fatalf("Stats = %+v, want 2 live, at least 124 bytes", got)
	}
	if s, err = memory.ResizeSlice(a, s, 10_000); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got.Live != 2 || got.Bytes < 10_000+24 {
		t.Fatalf("Stats after resize = %+v", got)
	}
	if err := errors.Join(memory.Free(a, p), memory.FreeSlice(a, s)); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != (memory.Stats{}) {
		t.Fatalf("Stats after free = %+v, want zero", got)
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
				*p.Value() = point{X: int64(g), Y: int64(i)}
				if v := p.Value(); v.X != int64(g) || v.Y != int64(i) {
					t.Errorf("p = %+v, want {%d %d}", *v, g, i)
				}
				if err := memory.Free(a, p); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if s := a.Stats(); s.Live != 0 {
		t.Fatalf("Stats = %+v, want none live", s)
	}
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
