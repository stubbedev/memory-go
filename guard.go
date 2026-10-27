//go:build !memory_unchecked

package memory

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

// Checked reports whether the misuse guards are compiled in, which they are
// unless the build sets the memory_unchecked tag.
const Checked = true

const (
	headerSize = int(unsafe.Sizeof(header{}))
	liveMagic  = 0xa110ca7e
	freedMagic = 0xf7eedf7e
)

type header struct {
	magic uint32
	owner uint32
	size  int64
}

func headerOf(p unsafe.Pointer) *header { return (*header)(unsafe.Add(p, -headerSize)) }

func checkLive(p unsafe.Pointer, size int) {
	switch h := headerOf(p); {
	case h.magic != liveMagic:
		panic(fmt.Sprintf("memory: use of %#x after it was freed", uintptr(p)))
	case h.size != int64(size):
		panic(fmt.Sprintf("memory: use of %#x after it was resized", uintptr(p)))
	}
}

var owners atomic.Uint32

type guard struct {
	busy atomic.Bool
	id   uint32
}

func (g *guard) enter() {
	if !g.busy.CompareAndSwap(false, true) {
		panic("memory: Local used by several goroutines at once; use Shared")
	}
	for g.id == 0 {
		g.id = owners.Add(1)
	}
}

func (g *guard) exit() { g.busy.Store(false) }

func (g *guard) reset() { g.id = 0 }

func (g *guard) stamp(base unsafe.Pointer, size int) unsafe.Pointer {
	*(*header)(base) = header{magic: liveMagic, owner: g.id, size: int64(size)}
	return unsafe.Add(base, headerSize)
}

func (g *guard) verify(p unsafe.Pointer, size int) {
	h := headerOf(p)
	switch {
	case h.magic == freedMagic:
		panic(fmt.Sprintf("memory: %#x is already freed", uintptr(p)))
	case h.magic != liveMagic:
		panic(fmt.Sprintf("memory: %#x is not a live allocation: already freed, or never allocated here", uintptr(p)))
	case h.owner != g.id:
		panic(fmt.Sprintf("memory: %#x belongs to a different allocator", uintptr(p)))
	case h.size != int64(size):
		panic(fmt.Sprintf("memory: %#x was resized since this handle was made", uintptr(p)))
	}
	h.magic = freedMagic
}
