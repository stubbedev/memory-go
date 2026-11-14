package memory_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	memory "github.com/stubbedev/memory-go"
)

func openRegion[T any](t *testing.T, path string, maxLen int) *memory.Region[T] {
	t.Helper()
	r, err := memory.OpenRegion[T](path, maxLen)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func closeRegion[T any](t *testing.T, r *memory.Region[T]) {
	t.Helper()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRegionFileReopens(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tree")
	const n = 3 << 16
	r := openRegion[regionNode](t, path, 1<<20)
	if r.Len() != 0 {
		t.Fatalf("new file Len = %d", r.Len())
	}
	var last memory.Index[regionNode]
	for i := range n {
		idx, err := r.Grow(1)
		if err != nil {
			t.Fatal(err)
		}
		p := r.Ptr(idx)
		p.Key, p.Left = uint64(i), last
		last = idx
	}
	if err := r.Sync(); err != nil {
		t.Fatal(err)
	}
	closeRegion(t, r)

	r = openRegion[regionNode](t, path, 1<<20)
	if r.Len() != n {
		t.Fatalf("reopened Len = %d, want %d", r.Len(), n)
	}
	count := 0
	for i := last; !i.IsNil(); i = r.Ptr(i).Left {
		if got, want := r.Ptr(i).Key, uint64(n-1-count); got != want {
			t.Fatalf("key %d = %d", want, got)
		}
		count++
	}
	if count != n {
		t.Fatalf("walked %d nodes, want %d", count, n)
	}
	if err := r.Truncate(10); err != nil {
		t.Fatal(err)
	}
	closeRegion(t, r)

	r = openRegion[regionNode](t, path, 20)
	defer closeRegion(t, r)
	if r.Len() != 10 || r.Elems()[9].Key != 9 {
		t.Fatalf("after truncate: Len = %d, last = %+v", r.Len(), r.Elems()[r.Len()-1])
	}
	if _, err := r.Grow(10); err != nil {
		t.Fatal(err)
	}
	for i, v := range r.Elems()[10:] {
		if v != (regionNode{}) {
			t.Fatalf("regrown element %d = %+v", 10+i, v)
		}
	}
	if r.File() == nil || r.File().Name() != path {
		t.Fatalf("File = %v", r.File())
	}
}

func TestRegionFileRejectsMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nodes")
	r := openRegion[regionNode](t, path, 100)
	if _, err := r.Grow(50); err != nil {
		t.Fatal(err)
	}
	closeRegion(t, r)

	if _, err := memory.OpenRegion[[3]uint64](path, 100); !errors.Is(err, memory.ErrRegionMismatch) ||
		!strings.Contains(err.Error(), "16-byte elements") || !strings.Contains(err.Error(), "24 bytes") {
		t.Fatalf("other size: err = %v", err)
	}
	type sameSize struct {
		Key  uint64
		A, B uint32
	}
	if _, err := memory.OpenRegion[sameSize](path, 100); !errors.Is(err, memory.ErrRegionMismatch) ||
		!strings.Contains(err.Error(), "field layout") {
		t.Fatalf("other layout: err = %v", err)
	}
	if _, err := memory.OpenRegion[regionNode](path, 49); !errors.Is(err, memory.ErrRegionFull) {
		t.Fatalf("maxLen below length: err = %v", err)
	}
	if _, err := memory.OpenRegion[string](path, 100); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("pointer type: err = %v", err)
	}

	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte(strings.Repeat("not a region ", 10)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.OpenRegion[regionNode](junk, 100); !errors.Is(err, memory.ErrNotRegionFile) {
		t.Fatalf("junk: err = %v", err)
	}
	short := filepath.Join(dir, "short")
	if err := os.WriteFile(short, []byte("memgoreg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.OpenRegion[regionNode](short, 100); !errors.Is(err, memory.ErrNotRegionFile) {
		t.Fatalf("short: err = %v", err)
	}
	if _, err := memory.OpenRegion[regionNode](filepath.Join(dir, "missing", "x"), 100); !errors.Is(err, os.ErrNotExist) ||
		!strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing dir: err = %v", err)
	}
}

func faults(t *testing.T, f func()) (faulted bool) {
	t.Helper()
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); !ok {
				t.Fatalf("panic = %v, want a fault", r)
			}
			faulted = true
		}
	}()
	f()
	return false
}

func TestRegionFreeze(t *testing.T) {
	t.Parallel()

	anon, err := memory.NewRegion[uint64](1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRegion(t, anon)
	file := openRegion[uint64](t, filepath.Join(t.TempDir(), "frozen"), 1<<20)
	defer closeRegion(t, file)

	for name, r := range map[string]*memory.Region[uint64]{"anonymous": anon, "file": file} {
		if _, err := r.Grow(1000); err != nil {
			t.Fatal(err)
		}
		xs := r.Elems()
		xs[999] = 7
		if err := r.Freeze(); err != nil {
			t.Fatal(err)
		}
		if !r.Frozen() || xs[999] != 7 {
			t.Fatalf("%s: frozen region unreadable", name)
		}
		if !faults(t, func() { xs[999] = 8 }) {
			t.Fatalf("%s: write to frozen region did not fault", name)
		}
		if _, err := r.Grow(1); !errors.Is(err, memory.ErrFrozen) {
			t.Fatalf("%s: Grow: err = %v", name, err)
		}
		if err := r.Truncate(1); !errors.Is(err, memory.ErrFrozen) {
			t.Fatalf("%s: Truncate: err = %v", name, err)
		}
		if err := r.Populate(); !errors.Is(err, memory.ErrFrozen) {
			t.Fatalf("%s: Populate: err = %v", name, err)
		}
		if err := r.Thaw(); err != nil {
			t.Fatal(err)
		}
		if faults(t, func() { xs[999] = 9 }) || xs[999] != 9 {
			t.Fatalf("%s: write after Thaw failed", name)
		}
		if _, err := r.Grow(1); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFreezeSlice(t *testing.T) {
	t.Parallel()

	var l memory.Local
	defer l.Close()
	small, err := memory.MakeSlice[byte](&l, 8<<10)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.FreezeSlice(&l, small); !errors.Is(err, memory.ErrSharesPages) {
		t.Fatalf("small: err = %v", err)
	}
	for name, a := range map[string]memory.Allocator{"local": &l, "faulty": memory.NewFaulty(&l)} {
		big, err := memory.MakeSlice[byte](a, 8<<10+1)
		if err != nil {
			t.Fatal(err)
		}
		big.Elems()[0] = 1
		if err := memory.FreezeSlice(a, big); err != nil {
			t.Fatal(err)
		}
		if !faults(t, func() { big.Elems()[0] = 2 }) {
			t.Fatalf("%s: write to frozen slice did not fault", name)
		}
		for _, n := range []int{1 << 20, 100} {
			if _, err := memory.ResizeSlice(a, big, n); !errors.Is(err, memory.ErrFrozen) {
				t.Fatalf("%s: resize to %d: err = %v", name, n, err)
			}
		}
		if err := memory.ThawSlice(a, big); err != nil {
			t.Fatal(err)
		}
		big.Elems()[0] = 3
		if err := memory.FreezeSlice(a, big); err != nil {
			t.Fatal(err)
		}
		if err := memory.FreeSlice(a, big); err != nil {
			t.Fatal(err)
		}
		again, err := memory.MakeSlice[byte](a, 8<<10+1)
		if err != nil {
			t.Fatal(err)
		}
		again.Elems()[0] = 4
		if err := memory.FreeSlice(a, again); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFreezeSliceShared(t *testing.T) {
	t.Parallel()

	var s memory.Shared
	defer s.Close()
	arena := memory.NewArena(&s)
	defer arena.Free()
	for name, a := range map[string]memory.Allocator{"shared": &s, "arena": arena} {
		big, err := memory.MakeSlice[uint64](a, 1<<12)
		if err != nil {
			t.Fatal(err)
		}
		if err := memory.FreezeSlice(a, big); err != nil {
			t.Fatal(err)
		}
		if !faults(t, func() { big.Elems()[1] = 1 }) {
			t.Fatalf("%s: write did not fault", name)
		}
		if _, err := memory.ResizeSlice(a, big, 10); !errors.Is(err, memory.ErrFrozen) {
			t.Fatalf("%s: shrink below the small limit: err = %v", name, err)
		}
		if err := memory.FreeSlice(a, big); err != nil {
			t.Fatal(err)
		}
	}
}
