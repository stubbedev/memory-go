package memory_test

import (
	"runtime"
	"sync/atomic"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
)

//go:noinline
func ownAndDrop(t *testing.T, a *memory.Shared, collected *atomic.Int32) {
	t.Helper()
	p, err := memory.New[[4]int](a)
	if err != nil {
		t.Fatal(err)
	}
	s, err := memory.MakeSlice[int](a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	str, err := memory.NewString(a, "owned")
	if err != nil {
		t.Fatal(err)
	}
	op, osl, ostr := memory.Own(a, p), memory.Own(a, s), memory.Own(a, str)
	op.Handle().Value()[3] = 1
	osl.Handle().Elems()[1<<19] = 1
	if ostr.Handle().String() != "owned" {
		t.Fatal("owned String lost its text")
	}
	count := func(c *atomic.Int32) { c.Add(1) }
	runtime.AddCleanup(op, count, collected)
	runtime.AddCleanup(osl, count, collected)
	runtime.AddCleanup(ostr, count, collected)
}

func TestOwnedFreesOnCollection(t *testing.T) {
	a := memtest.Shared(t)
	var collected atomic.Int32
	ownAndDrop(t, a, &collected)
	if got := a.Stats().Live; got != 3 {
		t.Fatalf("Live = %d, want 3", got)
	}
	collectUntil(t, "freeing unreachable owners", func() bool { return a.Stats().Live == 0 })
}

func TestOwnedCloseFreesOnceAndCancelsCleanup(t *testing.T) {
	a := memtest.Shared(t)
	p, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	o := memory.Own(a, p)
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats().Live; got != 0 {
		t.Fatalf("Live after Close = %d, want 0", got)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if !o.Handle().IsNil() {
		t.Fatal("Handle after Close is not nil")
	}
	var collected atomic.Bool
	runtime.AddCleanup(o, func(c *atomic.Bool) { c.Store(true) }, &collected)
	o = nil
	_ = o
	q, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, "collecting the closed owner", collected.Load)
	for range 5 {
		runtime.GC()
	}
	*q.Value() = 1
	if got := a.Stats().Live; got != 1 {
		t.Fatalf("Live = %d, want 1: a closed owner freed again", got)
	}
	if err := memory.Free(a, q); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedOutlivingItsSharedIsLeftAlone(t *testing.T) {
	a := memtest.Shared(t)
	var collected atomic.Int32
	ownAndDrop(t, a, &collected)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	p, err := memory.New[[4]int](a)
	if err != nil {
		t.Fatal(err)
	}
	collectUntil(t, "collecting owners from before Close", func() bool { return collected.Load() == 3 })
	for range 5 {
		runtime.GC()
	}
	p.Value()[3] = 1
	if got := a.Stats().Live; got != 1 {
		t.Fatalf("Live = %d, want 1: a stale owner freed a new allocation", got)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
}
