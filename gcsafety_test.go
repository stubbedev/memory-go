package memory_test

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
	"github.com/stubbedev/memory-go/pool"
)

type gcNode struct{ next *gcNode }

var (
	gcLive     []*gcNode
	gcLiveOnce sync.Once
)

func holdLongMarkPhases() {
	gcLiveOnce.Do(func() {
		var last *gcNode
		for range 1 << 20 {
			last = &gcNode{next: last}
			gcLive = append(gcLive, last)
		}
	})
}

func freedHeapAddresses() []uintptr {
	out := make([]uintptr, 0, 64)
	for range cap(out) {
		b := make([]byte, 1<<20)
		out = append(out, uintptr(unsafe.Pointer(&b[0]))+1024)
		runtime.KeepAlive(b)
	}
	runtime.GC()
	runtime.GC()
	return out
}

func whileCollecting(t *testing.T, f func(bad []uintptr)) {
	t.Helper()
	holdLongMarkPhases()
	bad := freedHeapAddresses()
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
				runtime.GC()
			}
		}
	})
	defer wg.Wait()
	defer close(done)
	f(bad)
}

const gcRounds = 400_000

func TestFreeingDataThatLooksLikeAPointerDuringGC(t *testing.T) {
	a := memtest.Local(t)
	whileCollecting(t, func(bad []uintptr) {
		for i := range gcRounds {
			p, err := memory.New[[4]uintptr](a)
			if err != nil {
				t.Fatal(err)
			}
			x := bad[i%len(bad)]
			*p.Value() = [4]uintptr{x, x, x, x}
			if err := memory.Free(a, p); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestFreeingToAnotherShardDuringGC(t *testing.T) {
	a := memtest.Shared(t)
	whileCollecting(t, func(bad []uintptr) {
		work := make(chan memory.Ptr[[4]uintptr], 64)
		var wg sync.WaitGroup
		wg.Go(func() {
			for p := range work {
				if err := memory.Free(a, p); err != nil {
					t.Error(err)
				}
			}
		})
		for i := range gcRounds {
			p, err := memory.New[[4]uintptr](a)
			if err != nil {
				t.Fatal(err)
			}
			x := bad[i%len(bad)]
			*p.Value() = [4]uintptr{x, x, x, x}
			work <- p
		}
		close(work)
		wg.Wait()
	})
}

func TestPuttingDataThatLooksLikeAPointerDuringGC(t *testing.T) {
	a := memtest.Local(t)
	p, err := pool.New[[4]uintptr](a, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	whileCollecting(t, func(bad []uintptr) {
		for i := range gcRounds {
			it, err := p.GetUninit()
			if err != nil {
				t.Fatal(err)
			}
			x := bad[i%len(bad)]
			*it.Value() = [4]uintptr{x, x, x, x}
			p.Put(it)
		}
	})
}

type holdsHandle struct {
	A, B memory.Ptr[int]
}

func TestUninitHandlesOverStaleBytesDuringGC(t *testing.T) {
	a := memtest.Local(t)
	whileCollecting(t, func(bad []uintptr) {
		for i := range gcRounds {
			old, err := memory.New[[2]uintptr](a)
			if err != nil {
				t.Fatal(err)
			}
			x := bad[i%len(bad)]
			*old.Value() = [2]uintptr{x, x}
			if err := memory.Free(a, old); err != nil {
				t.Fatal(err)
			}
			p, err := memory.NewUninit[holdsHandle](a)
			if err != nil {
				t.Fatal(err)
			}
			*p.Value() = holdsHandle{}
			if err := memory.Free(a, p); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func TestUninitOfHandlesIsZeroedButPlainDataIsNot(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const stale = 0x5a5a5a5a
	dirty, err := memory.New[[4]uintptr](a)
	if err != nil {
		t.Fatal(err)
	}
	*dirty.Value() = [4]uintptr{stale, stale, stale, stale}
	if err := memory.Free(a, dirty); err != nil {
		t.Fatal(err)
	}
	plain, err := memory.NewUninit[[4]uintptr](a)
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.Value()[3]; got != stale {
		t.Fatalf("plain NewUninit word = %#x, want it left uncleared", got)
	}
	if err := memory.Free(a, plain); err != nil {
		t.Fatal(err)
	}
	handles, err := memory.NewUninit[[2]holdsHandle](a)
	if err != nil {
		t.Fatal(err)
	}
	if !handles.Value()[1].B.IsNil() {
		t.Fatal("NewUninit of a type with handles left stale bytes")
	}
	if err := memory.Free(a, handles); err != nil {
		t.Fatal(err)
	}
}
