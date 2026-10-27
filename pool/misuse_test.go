//go:build !memory_unchecked

package pool_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/pool"
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

	a := memtest.Local(t)
	p, err := pool.New[particle](a, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	q, err := pool.New[particle](a, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Free()
	it, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "not live in this pool", func() { q.Put(it) })
	p.Put(it)
	mustPanic(t, "not live in this pool", func() { p.Put(it) })
	mustPanic(t, "after Put", func() { it.Value() })
}
