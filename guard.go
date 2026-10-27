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
	liveMagic  = 0xa110ca7ed0ff4ea9
	freedMagic = 0xf7eedf7eedf7eed5
)

type header struct {
	owner uint64
	magic uint64
}

func headerOf(p unsafe.Pointer) *header { return (*header)(unsafe.Add(p, -headerSize)) }

func checkLive(p unsafe.Pointer) {
	if headerOf(p).magic != liveMagic {
		panic(fmt.Sprintf("memory: use of %#x after it was freed", uintptr(p)))
	}
}

var owners atomic.Uint64

type guard struct {
	busy atomic.Bool
	id   uint64
}

func (g *guard) enter() {
	if !g.busy.CompareAndSwap(false, true) {
		panic("memory: Local used by several goroutines at once; use Shared")
	}
	if g.id == 0 {
		g.id = owners.Add(1)
	}
}

func (g *guard) exit() { g.busy.Store(false) }

func (g *guard) reset() { g.id = 0 }

func (g *guard) stamp(base unsafe.Pointer) unsafe.Pointer {
	*(*header)(base) = header{owner: g.id, magic: liveMagic}
	return unsafe.Add(base, headerSize)
}

func (g *guard) verify(p unsafe.Pointer) {
	h := headerOf(p)
	switch {
	case h.magic == freedMagic:
		panic(fmt.Sprintf("memory: %#x is already freed", uintptr(p)))
	case h.magic != liveMagic:
		panic(fmt.Sprintf("memory: %#x is not a live allocation: already freed, or never allocated here", uintptr(p)))
	case h.owner != g.id:
		panic(fmt.Sprintf("memory: %#x belongs to a different allocator", uintptr(p)))
	}
	h.magic = freedMagic
}
