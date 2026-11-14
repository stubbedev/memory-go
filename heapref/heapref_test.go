package heapref_test

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"
	"time"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/heapref"
	"github.com/stubbedev/memory-go/internal/testkit"
	"github.com/stubbedev/memory-go/memtest"
	"github.com/stubbedev/memory-go/vector"
)

func TestHandlesLiveOffHeap(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	r, err := memory.New[heapref.Ref[func()]](a)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Free(a, r)
	w, err := memory.New[heapref.WeakRef[int]](a)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Free(a, w)
	id, err := memory.New[heapref.FuncID[func()]](a)
	if err != nil {
		t.Fatal(err)
	}
	defer memory.Free(a, id)
	v, err := vector.New[heapref.Ref[any]](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	m, err := hashmap.New[heapref.Ref[any], heapref.FuncID[func()]](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
}

func TestTable(t *testing.T) {
	t.Parallel()

	var tab heapref.Table[any]
	if !(heapref.Ref[any]{}).IsNil() {
		t.Fatal("zero Ref is not nil")
	}
	m := map[string]int{"a": 1}
	r1, err := tab.Put(m)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := tab.Put(fmt.Stringer(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if r1.IsNil() || r2.IsNil() || r1 == r2 || tab.Len() != 2 {
		t.Fatalf("r1 = %v, r2 = %v, Len = %d", r1, r2, tab.Len())
	}
	if got := tab.Get(r1).(map[string]int)["a"]; got != 1 {
		t.Fatalf("Get(r1)[a] = %d, want 1", got)
	}
	tab.Set(r2, "replaced")
	if got := tab.Get(r2); got != "replaced" {
		t.Fatalf("Get(r2) = %v, want replaced", got)
	}
	tab.Delete(r1)
	r3, err := tab.Put(3)
	if err != nil {
		t.Fatal(err)
	}
	if r3 == r1 || tab.Get(r3) != 3 || tab.Len() != 2 {
		t.Fatalf("reused r3 = %v, r1 = %v, Len = %d", r3, r1, tab.Len())
	}
}

func TestWeak(t *testing.T) {
	var w heapref.Weak[[64]byte]
	kept := new([64]byte)
	r, err := w.Put(kept)
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := w.Put(new([64]byte))
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.GC()
	if w.Get(r) != kept {
		t.Fatal("Get of a live target did not return it")
	}
	if w.Get(dropped) != nil {
		t.Fatal("Get of a collected target is not nil")
	}
	w.Set(dropped, kept)
	if w.Get(dropped) != kept || w.Len() != 2 {
		t.Fatalf("after Set: Get = %p, Len = %d", w.Get(dropped), w.Len())
	}
	w.Delete(dropped)
	if w.Len() != 1 || dropped.IsNil() || !(heapref.WeakRef[int]{}).IsNil() {
		t.Fatalf("after Delete: Len = %d", w.Len())
	}
	runtime.KeepAlive(kept)
}

func TestFuncs(t *testing.T) {
	t.Parallel()

	var fs heapref.Funcs[func() int]
	one, err := fs.Register(func() int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	two, err := fs.Register(func() int { return 2 })
	if err != nil {
		t.Fatal(err)
	}
	if one.IsNil() || !heapref.FuncID[func() int](0).IsNil() || fs.Len() != 2 {
		t.Fatalf("one = %d, Len = %d", one, fs.Len())
	}
	if fs.Get(one)() != 1 || fs.Get(two)() != 2 {
		t.Fatal("Get returned the wrong function")
	}
}

type payload struct{ n [16]int }

func collected(done <-chan struct{}) bool {
	for range 100 {
		runtime.GC()
		select {
		case <-done:
			return true
		case <-time.After(time.Millisecond):
		}
	}
	return false
}

func TestValueLivesUntilDelete(t *testing.T) {
	a := memtest.Local(t)
	var tab heapref.Table[*payload]
	type holder struct{ Ref heapref.Ref[*payload] }
	h, err := memory.New[holder](a)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	func() {
		p := &payload{}
		p.n[0] = 42
		runtime.AddCleanup(p, func(c chan struct{}) { close(c) }, done)
		r, err := tab.Put(p)
		if err != nil {
			t.Fatal(err)
		}
		h.Value().Ref = r
	}()
	if collected(done) {
		t.Fatal("a value held only through the Table was collected")
	}
	if got := tab.Get(h.Value().Ref).n[0]; got != 42 {
		t.Fatalf("value after GC = %d, want 42", got)
	}
	tab.Delete(h.Value().Ref)
	if !collected(done) {
		t.Fatal("a deleted value was not collected")
	}
	if err := memory.Free(a, h); err != nil {
		t.Fatal(err)
	}
}

func FuzzMatchesModel(f *testing.F) {
	f.Add(testkit.Seed(11, 1<<14))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		var tab heapref.Table[string]
		model := map[heapref.Ref[string]]string{}
		var refs []heapref.Ref[string]
		ops := testkit.NewOps(data)
		for step := 0; ops.More(); step++ {
			switch op := ops.Int(4); {
			case op < 2 || len(refs) == 0:
				v := strconv.FormatUint(ops.Uint64(), 10)
				r, err := tab.Put(v)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := model[r]; ok || r.IsNil() {
					t.Fatalf("step %d: Put returned live or nil Ref %v", step, r)
				}
				model[r] = v
				refs = append(refs, r)
			case op == 2:
				r := refs[ops.Int(len(refs))]
				v := strconv.FormatUint(ops.Uint64(), 10)
				tab.Set(r, v)
				model[r] = v
			default:
				i := ops.Int(len(refs))
				tab.Delete(refs[i])
				delete(model, refs[i])
				refs[i] = refs[len(refs)-1]
				refs = refs[:len(refs)-1]
			}
			if tab.Len() != len(model) {
				t.Fatalf("step %d: Len = %d, want %d", step, tab.Len(), len(model))
			}
		}
		for r, want := range model {
			if got := tab.Get(r); got != want {
				t.Fatalf("Get(%v) = %q, want %q", r, got, want)
			}
		}
	})
}
