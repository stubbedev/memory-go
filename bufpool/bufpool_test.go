package bufpool_test

import (
	"errors"
	"testing"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/bufpool"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func TestSizesAndAlignment(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := bufpool.New(a, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 4096, 4097, 8192, 8193, 64 << 10, 1 << 20, 5 << 20} {
		b, err := p.Get(n)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Bytes()) != n || b.Cap() < n || b.Cap()&(b.Cap()-1) != 0 {
			t.Fatalf("n=%d: len %d cap %d", n, len(b.Bytes()), b.Cap())
		}
		if addr := uintptr(unsafe.Pointer(&b.Bytes()[0])); addr%4096 != 0 {
			t.Fatalf("n=%d: address %#x not 4 KiB aligned", n, addr)
		}
		clear(b.Bytes())
		if err := p.Put(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Trim(); err != nil {
		t.Fatal(err)
	}
}

func TestReusesReturnedBuffers(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := bufpool.New(a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Get(5000)
	if err != nil {
		t.Fatal(err)
	}
	first := &b.Bytes()[0]
	if err := p.Put(b); err != nil {
		t.Fatal(err)
	}
	c, err := p.Get(8000)
	if err != nil {
		t.Fatal(err)
	}
	if &c.Bytes()[0] != first {
		t.Fatal("buffer of the same class was not reused")
	}
	if err := p.Put(c); err != nil {
		t.Fatal(err)
	}
	if err := p.Trim(); err != nil {
		t.Fatal(err)
	}
	if got := a.Stats(); got != (memory.Stats{}) {
		t.Fatalf("Stats after Trim = %+v", got)
	}
}

func TestRetentionIsBounded(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := bufpool.New(a, 4096)
	if err != nil {
		t.Fatal(err)
	}
	var bufs []bufpool.Buf
	for range 4 {
		b, err := p.Get(4096)
		if err != nil {
			t.Fatal(err)
		}
		bufs = append(bufs, b)
	}
	for _, b := range bufs {
		if err := p.Put(b); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.Stats().Live; got != 1 {
		t.Fatalf("%d buffers kept, want 1", got)
	}
	if err := p.Trim(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsMisuse(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := bufpool.New(a, -1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Fatalf("New(-1): %v", err)
	}
	p, err := bufpool.New(a, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(-1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Fatalf("Get(-1): %v", err)
	}
	if err := p.Put(bufpool.Buf{}); !errors.Is(err, bufpool.ErrForeign) {
		t.Fatalf("Put(zero): %v", err)
	}
	b, err := p.Get(100)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Put(b); err != nil {
		t.Fatal(err)
	}
	if memory.Checked {
		if err := p.Put(b); !errors.Is(err, bufpool.ErrForeign) {
			t.Fatalf("second Put: %v", err)
		}
	}
	if err := p.Trim(); err != nil {
		t.Fatal(err)
	}
}
