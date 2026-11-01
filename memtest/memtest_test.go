package memtest_test

import (
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
)

type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper() {}

func (r *recorder) Errorf(string, ...any) { r.failed = true }

func (r *recorder) Cleanup(f func()) { r.TB.Cleanup(func() { f(); r.check() }) }

func (r *recorder) check() {
	if !r.failed {
		r.Error("leak went unreported")
	}
}

func TestReportsLeaks(t *testing.T) {
	t.Parallel()

	t.Run("local", func(t *testing.T) {
		t.Parallel()
		if _, err := memory.New[int](memtest.Local(&recorder{TB: t})); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("shared", func(t *testing.T) {
		t.Parallel()
		if _, err := memory.New[int](memtest.Shared(&recorder{TB: t})); err != nil {
			t.Fatal(err)
		}
	})
}

func TestQuietWithoutLeaks(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := memory.New[int](a)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.Free(a, p); err != nil {
		t.Fatal(err)
	}
}
