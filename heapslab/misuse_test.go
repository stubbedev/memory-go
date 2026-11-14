//go:build !memory_unchecked

package heapslab_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/memory-go/heapslab"
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

	p, err := heapslab.NewPool[node](8)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	q, err := heapslab.NewPool[node](8)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Free()
	it := p.Get()
	mustPanic(t, "heapslab: Put of", func() { q.Put(it) })
	p.Put(it)
	mustPanic(t, "not live in this pool", func() { p.Put(it) })
	mustPanic(t, "heapslab: use of", func() { it.Value() })

	stale := p.Get()
	p.Reset()
	mustPanic(t, "after Put", func() { stale.Value() })
	mustPanic(t, "not live in this pool", func() { p.Put(stale) })

	freed := q.Get()
	q.Free()
	mustPanic(t, "after Put", func() { freed.Value() })
}
