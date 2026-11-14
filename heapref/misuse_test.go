//go:build !memory_unchecked

package heapref_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/memory-go/heapref"
)

func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		msg, _ := recover().(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic = %q, want one containing %q", msg, want)
		}
	}()
	f()
}

func TestMisusePanics(t *testing.T) {
	t.Parallel()

	var tab, other heapref.Table[func()]
	r, err := tab.Put(func() {})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Put(func() {}); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "Get of a nil Ref", func() { tab.Get(heapref.Ref[func()]{}) })
	mustPanic(t, "deleted or is from another Table", func() { other.Get(r) })
	mustPanic(t, "Delete of a Ref that was deleted", func() { other.Delete(r) })
	tab.Delete(r)
	mustPanic(t, "Get of a Ref that was deleted", func() { tab.Get(r) })
	mustPanic(t, "Set of a Ref that was deleted", func() { tab.Set(r, nil) })
	mustPanic(t, "Delete of a Ref that was deleted", func() { tab.Delete(r) })
	reused, err := tab.Put(func() {})
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "Get of a Ref that was deleted", func() { tab.Get(r) })
	tab.Get(reused)
	mustPanic(t, "from another Table", func() { (&heapref.Table[func()]{}).Get(reused) })

	var w heapref.Weak[int]
	mustPanic(t, "Get of a nil Ref", func() { w.Get(heapref.WeakRef[int]{}) })

	var fs, more heapref.Funcs[func()]
	id, err := more.Register(func() {})
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "Get of a nil Ref", func() { fs.Get(0) })
	mustPanic(t, "from another Table", func() { fs.Get(id) })
}
