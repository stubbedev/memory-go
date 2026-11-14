package union_test

import (
	"errors"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/testkit"
	"github.com/stubbedev/memory-go/memtest"
	"github.com/stubbedev/memory-go/union"
	"github.com/stubbedev/memory-go/vector"
)

type pair struct {
	A int32
	B float64
}

type wide struct {
	Tag  uint8
	N    uint64
	Vals [8]int64
}

type named struct {
	ID   uint32
	Name memory.String
}

type mixed struct {
	Flag  bool
	Big   [5]uint64
	Label memory.String
}

func roundTrip[T comparable](t *testing.T, a memory.Allocator, r *union.Registry, x T) {
	t.Helper()
	id, err := union.Register[T](r)
	if err != nil {
		t.Fatal(err)
	}
	v, err := union.Store(a, r, x)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := union.Free(a, v); err != nil {
			t.Fatal(err)
		}
	}()
	if v.ID() != id || v.IsNil() {
		t.Fatalf("ID = %d, IsNil = %v, want %d", v.ID(), v.IsNil(), id)
	}
	got, ok := union.Load[T](r, v)
	if !ok || got != x {
		t.Fatalf("Load = %v, %v, want %v", got, ok, x)
	}
	if got := union.Interface(r, v); got != any(x) {
		t.Fatalf("Interface = %v, want %v", got, x)
	}
	if got := union.TypeOf(r, v); got != reflect.TypeFor[T]() {
		t.Fatalf("TypeOf = %v, want %v", got, reflect.TypeFor[T]())
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	roundTrip(t, a, &r, 42)
	roundTrip(t, a, &r, int8(-7))
	roundTrip(t, a, &r, uint64(math.MaxUint64))
	roundTrip(t, a, &r, math.Pi)
	roundTrip(t, a, &r, complex(1.5, -2.5))
	roundTrip(t, a, &r, true)
	roundTrip(t, a, &r, struct{}{})
	roundTrip(t, a, &r, [0]int{})
	roundTrip(t, a, &r, pair{A: -3, B: 0.25})
	roundTrip(t, a, &r, [union.InlineSize]byte{1, 2, 3, union.InlineSize - 1: 9})
	roundTrip(t, a, &r, [union.InlineSize + 1]byte{1, union.InlineSize: 9})
	roundTrip(t, a, &r, [3]uint16{1, 2, 3})
	roundTrip(t, a, &r, wide{Tag: 1, N: math.MaxUint64, Vals: [8]int64{1, -1, 7: math.MinInt64}})
}

func TestHandlePayloads(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	if _, err := union.Register[named](&r); err != nil {
		t.Fatal(err)
	}
	if _, err := union.Register[mixed](&r); err != nil {
		t.Fatal(err)
	}
	name, err := memory.NewString(a, "small")
	if err != nil {
		t.Fatal(err)
	}
	defer memory.FreeString(a, name)
	label, err := memory.NewString(a, "large")
	if err != nil {
		t.Fatal(err)
	}
	defer memory.FreeString(a, label)

	small, err := union.Store(a, &r, named{ID: 9, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	defer union.Free(a, small)
	large, err := union.Store(a, &r, mixed{Flag: true, Big: [5]uint64{4: 4}, Label: label})
	if err != nil {
		t.Fatal(err)
	}
	defer union.Free(a, large)

	n, ok := union.Load[named](&r, small)
	if !ok || n.ID != 9 || n.Name.String() != "small" {
		t.Fatalf("Load[named] = %+v, %v", n, ok)
	}
	m, ok := union.Interface(&r, large).(mixed)
	if !ok || !m.Flag || m.Big[4] != 4 || m.Label.String() != "large" {
		t.Fatalf("Interface = %+v, %v", m, ok)
	}
}

func TestWrongType(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	for _, err := range []error{
		func() error { _, err := union.Register[int](&r); return err }(),
		func() error { _, err := union.Register[uint](&r); return err }(),
		func() error { _, err := union.Register[[32]byte](&r); return err }(),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	v, err := union.Store(a, &r, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := union.Load[uint](&r, v); ok || got != 0 {
		t.Fatalf("Load[uint] = %d, %v", got, ok)
	}
	if _, ok := union.Load[[32]byte](&r, v); ok {
		t.Fatal("Load[[32]byte] of an int reported true")
	}
	if _, ok := union.Load[float32](&r, v); ok {
		t.Fatal("Load of an unregistered type reported true")
	}

	var zero union.Value
	if !zero.IsNil() || zero.ID() != 0 {
		t.Fatal("zero Value is not nil")
	}
	if _, ok := union.Load[int](&r, zero); ok {
		t.Fatal("Load of the zero Value reported true")
	}
	if union.Interface(&r, zero) != nil || union.TypeOf(&r, zero) != nil {
		t.Fatal("zero Value has a type")
	}
	if err := union.Free(a, zero); err != nil {
		t.Fatal(err)
	}

	var other union.Registry
	if union.Interface(&other, v) != nil || union.TypeOf(&other, v) != nil {
		t.Fatal("empty Registry knows an ID")
	}
	if _, ok := union.Load[int](&other, v); ok {
		t.Fatal("empty Registry loaded a value")
	}
}

func TestRejectsPointers(t *testing.T) {
	t.Parallel()

	var r union.Registry
	for name, register := range map[string]func(*union.Registry) (uint32, error){
		"pointer":   union.Register[*int],
		"string":    union.Register[string],
		"slice":     union.Register[[]byte],
		"interface": union.Register[any],
		"field":     union.Register[struct{ S string }],
		"map":       union.Register[map[int]int],
	} {
		id, err := register(&r)
		if !errors.Is(err, memory.ErrHoldsPointers) || id != 0 {
			t.Errorf("%s: Register = %d, %v", name, id, err)
		}
		if err != nil && !strings.HasPrefix(err.Error(), "union: register ") {
			t.Errorf("%s: error %q", name, err)
		}
	}
}

func TestUnregistered(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	if _, err := union.Store(a, &r, 1.5); !errors.Is(err, union.ErrUnregistered) {
		t.Fatalf("Store = %v", err)
	}
}

func TestStoreInterface(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	if _, err := union.Register[pair](&r); err != nil {
		t.Fatal(err)
	}
	if _, err := union.Register[wide](&r); err != nil {
		t.Fatal(err)
	}
	for _, x := range []any{pair{A: 2, B: 3}, wide{Tag: 4, Vals: [8]int64{7: 5}}} {
		v, err := union.StoreInterface(a, &r, x)
		if err != nil {
			t.Fatal(err)
		}
		if got := union.Interface(&r, v); got != x {
			t.Fatalf("Interface = %v, want %v", got, x)
		}
		if err := union.Free(a, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range []any{nil, 1, "text"} {
		if _, err := union.StoreInterface(a, &r, x); !errors.Is(err, union.ErrUnregistered) {
			t.Fatalf("StoreInterface(%v) = %v", x, err)
		}
	}
}

func TestRegisterIdempotent(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	first, err := union.Register[pair](&r)
	if err != nil {
		t.Fatal(err)
	}
	v, err := union.Store(a, &r, pair{A: 1})
	if err != nil {
		t.Fatal(err)
	}
	later, err := union.Register[wide](&r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := union.Register[pair](&r)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || later != 2 || again != first {
		t.Fatalf("IDs %d, %d, %d", first, later, again)
	}
	if got, ok := union.Load[pair](&r, v); !ok || got.A != 1 {
		t.Fatalf("Load after Register = %+v, %v", got, ok)
	}
}

func TestConcurrentRegister(t *testing.T) {
	t.Parallel()

	var r union.Registry
	ids := make(chan uint32, 8)
	for range cap(ids) {
		go func() {
			id, err := union.Register[pair](&r)
			if err != nil {
				t.Error(err)
			}
			if _, err := union.Register[int](&r); err != nil {
				t.Error(err)
			}
			ids <- id
		}()
	}
	want := <-ids
	for range cap(ids) - 1 {
		if got := <-ids; got != want {
			t.Fatalf("ID %d, want %d", got, want)
		}
	}
}

func TestLargePayloadsFreed(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	var r union.Registry
	if _, err := union.Register[wide](&r); err != nil {
		t.Fatal(err)
	}
	v, err := union.Store(a, &r, wide{N: 3})
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Stats(); s.Live != 1 {
		t.Fatalf("Live = %d, want 1", s.Live)
	}
	if err := union.Free(a, v); err != nil {
		t.Fatal(err)
	}
	if s := a.Stats(); s.Live != 0 {
		t.Fatalf("Live = %d after Free", s.Live)
	}
}

func TestStoreFails(t *testing.T) {
	t.Parallel()

	f := memory.NewFaulty(memtest.Local(t))
	var r union.Registry
	if _, err := union.Register[wide](&r); err != nil {
		t.Fatal(err)
	}
	f.FailAllocsAfter(0)
	if v, err := union.Store(f, &r, wide{}); err == nil || !v.IsNil() {
		t.Fatalf("Store = %v, %v", v, err)
	}
}

func TestValueOffHeap(t *testing.T) {
	t.Parallel()

	if err := memory.CheckType[union.Value](); err != nil {
		t.Fatal(err)
	}
	a := memtest.Local(t)
	var r union.Registry
	if _, err := union.Register[named](&r); err != nil {
		t.Fatal(err)
	}
	if _, err := union.Register[wide](&r); err != nil {
		t.Fatal(err)
	}
	if _, err := union.Register[int64](&r); err != nil {
		t.Fatal(err)
	}
	vals, err := vector.New[union.Value](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	const n = 300
	var names []memory.String
	for i := range n {
		var v union.Value
		switch i % 3 {
		case 0:
			s, err := memory.NewString(a, strings.Repeat("x", i))
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, s)
			v, err = union.Store(a, &r, named{ID: uint32(i), Name: s})
			if err != nil {
				t.Fatal(err)
			}
		case 1:
			v, err = union.Store(a, &r, wide{N: uint64(i)})
		default:
			v, err = union.Store(a, &r, int64(-i))
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := vals.Push(v); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	runtime.GC()
	for i, v := range vals.All() {
		switch x := union.Interface(&r, v).(type) {
		case named:
			if i%3 != 0 || x.ID != uint32(i) || x.Name.String() != strings.Repeat("x", i) {
				t.Fatalf("%d: %+v", i, x)
			}
		case wide:
			if i%3 != 1 || x.N != uint64(i) {
				t.Fatalf("%d: %+v", i, x)
			}
		case int64:
			if i%3 != 2 || x != int64(-i) {
				t.Fatalf("%d: %d", i, x)
			}
		default:
			t.Fatalf("%d: %T", i, x)
		}
		if err := union.Free(a, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range names {
		if err := memory.FreeString(a, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := vals.Free(); err != nil {
		t.Fatal(err)
	}

	p, err := memory.New[union.Value](a)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
}

func FuzzMatchesModel(f *testing.F) {
	f.Add(testkit.Seed(7, 1<<12))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a := &memory.Local{}
		defer a.Close()
		var r union.Registry
		registers := []func(*union.Registry) (uint32, error){
			union.Register[uint8],
			union.Register[int64],
			union.Register[pair],
			union.Register[[3]uint16],
			union.Register[[union.InlineSize + 1]byte],
			union.Register[wide],
		}
		var vals []union.Value
		var model []any
		ops := testkit.NewOps(data)
		for ops.More() {
			switch op := ops.Int(4); {
			case op == 0 && len(vals) > 0:
				i := ops.Int(len(vals))
				if err := union.Free(a, vals[i]); err != nil {
					t.Fatal(err)
				}
				vals = append(vals[:i], vals[i+1:]...)
				model = append(model[:i], model[i+1:]...)
			case op == 1:
				if _, err := registers[ops.Int(len(registers))](&r); err != nil {
					t.Fatal(err)
				}
			default:
				x := ops.Uint64()
				var value any
				var v union.Value
				var err error
				switch ops.Int(6) {
				case 0:
					value = uint8(x)
					v, err = union.Store(a, &r, uint8(x))
				case 1:
					value = int64(x)
					v, err = union.Store(a, &r, int64(x))
				case 2:
					value = pair{A: int32(x), B: float64(x >> 8)}
					v, err = union.Store(a, &r, value.(pair))
				case 3:
					value = [3]uint16{uint16(x), uint16(x >> 16), uint16(x >> 32)}
					v, err = union.Store(a, &r, value.([3]uint16))
				case 4:
					value = [union.InlineSize + 1]byte{byte(x), union.InlineSize: byte(x >> 8)}
					v, err = union.Store(a, &r, value.([union.InlineSize + 1]byte))
				default:
					value = wide{Tag: uint8(x), N: x, Vals: [8]int64{7: int64(x)}}
					v, err = union.Store(a, &r, value.(wide))
				}
				if errors.Is(err, union.ErrUnregistered) {
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				vals = append(vals, v)
				model = append(model, value)
			}
		}
		for i, v := range vals {
			if got := union.Interface(&r, v); got != model[i] {
				t.Fatalf("%d: Interface = %v, want %v", i, got, model[i])
			}
			if got := union.TypeOf(&r, v); got != reflect.TypeOf(model[i]) {
				t.Fatalf("%d: TypeOf = %v, want %T", i, got, model[i])
			}
			if _, isInt := model[i].(int64); !isInt {
				if _, ok := union.Load[int64](&r, v); ok {
					t.Fatalf("%d: Load[int64] of %T reported true", i, model[i])
				}
			}
			if err := union.Free(a, v); err != nil {
				t.Fatal(err)
			}
		}
		if s := a.Stats(); s.Live != 0 {
			t.Fatalf("Live = %d", s.Live)
		}
	})
}
