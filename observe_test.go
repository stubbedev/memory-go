package memory_test

import (
	"bytes"
	"errors"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func TestFootprintFollowsLargeAllocations(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const n = 16 << 20
	s, err := memory.MakeSlice[byte](a, n)
	if err != nil {
		t.Fatal(err)
	}
	f := a.Footprint()
	if f.Mapped < n || f.Committed < n || f.Retained != 0 {
		t.Fatalf("Footprint with a live allocation = %+v", f)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
	if f = a.Footprint(); f.Retained < n {
		t.Fatalf("Footprint after free = %+v, want it retained", f)
	}
	if err := a.Trim(); err != nil {
		t.Fatal(err)
	}
	if f = a.Footprint(); f != (memory.Footprint{}) {
		t.Fatalf("Footprint after Trim = %+v, want zero", f)
	}
}

func TestSyncMemoryLimit(t *testing.T) {
	old := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(old)

	a := memtest.Local(t)
	const n = 8 << 20
	s, err := memory.MakeSlice[byte](a, n)
	if err != nil {
		t.Fatal(err)
	}
	const total = 1 << 30
	limit, err := memory.SyncMemoryLimit(total, a)
	if err != nil {
		t.Fatal(err)
	}
	if limit > total-n || limit != debug.SetMemoryLimit(-1) {
		t.Fatalf("limit = %d, runtime has %d", limit, debug.SetMemoryLimit(-1))
	}
	if _, err := memory.SyncMemoryLimit(n/2, a); !errors.Is(err, memory.ErrOverLimit) {
		t.Fatalf("err = %v, want ErrOverLimit", err)
	}
	if got := debug.SetMemoryLimit(-1); got != limit {
		t.Fatalf("limit changed to %d by a failed sync", got)
	}
	if err := memory.FreeSlice(a, s); err != nil {
		t.Fatal(err)
	}
}

func testProfile(name string) *pprof.Profile {
	if p := pprof.Lookup(name); p != nil {
		return p
	}
	return pprof.NewProfile(name)
}

func TestProfileTracksLiveAllocations(t *testing.T) {
	t.Parallel()

	for name, a := range allocators(t) {
		prof := testProfile("memory-go-test-" + name)
		switch a := a.(type) {
		case *memory.Local:
			a.SetProfile(prof)
		case *memory.Shared:
			a.SetProfile(prof)
		}
		p, err := memory.New[point](a)
		if err != nil {
			t.Fatal(err)
		}
		big, err := memory.MakeSlice[byte](a, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if got := prof.Count(); got != 2 {
			t.Fatalf("%s: Count = %d, want 2", name, got)
		}
		if big, err = memory.ResizeSlice(a, big, 2<<20); err != nil {
			t.Fatal(err)
		}
		if got := prof.Count(); got != 2 {
			t.Fatalf("%s: Count after resize = %d, want 2", name, got)
		}
		var out bytes.Buffer
		if err := prof.WriteTo(&out, 1); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "TestProfileTracksLiveAllocations") {
			t.Fatalf("%s: profile does not name the caller:\n%s", name, out.String())
		}
		if err := errors.Join(memory.Free(a, p), memory.FreeSlice(a, big)); err != nil {
			t.Fatal(err)
		}
		if got := prof.Count(); got != 0 {
			t.Fatalf("%s: Count after free = %d, want 0", name, got)
		}
	}
}
