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
	sideEntry   = 2
	trailerSize = int(unsafe.Sizeof(trailer{}))
	liveMagic   = 0xa110ca7e
	freedMagic  = 0xf7eedf7e
	movedMagic  = 0x30fed0ff
)

type trailer struct {
	magic uint32
	_     uint32
	size  int64
}

func trailerOffset(size int) int { return (size + trailerSize - 1) &^ (trailerSize - 1) }

func trailerOf(p unsafe.Pointer, size int) *trailer {
	return (*trailer)(unsafe.Add(p, trailerOffset(size)))
}

func (s *span) side(i uint32) *uint16 {
	return (*uint16)(unsafe.Add(unsafe.Pointer(s), sideOffset+sideEntry*i))
}

func (s *span) index(p unsafe.Pointer) uint32 {
	off := uint32(uintptr(p)&spanMask) - s.data
	return uint32(uint64(off) * uint64(s.inv) >> 32)
}

func checkLive(p unsafe.Pointer, size int) {
	if size > smallMax {
		checkTrailer(p, size)
		return
	}
	s := spanOf(p)
	if i := s.index(p); i >= s.slots || *s.side(i) != uint16(size) {
		checkDeadSlot(s, p, size)
	}
}

func checkDeadSlot(s *span, p unsafe.Pointer, size int) {
	if s.isBump() && s.holds(p, size) {
		return
	}
	if i := s.index(p); !s.isBump() && i < s.slots && *s.side(i) != 0 {
		panic(fmt.Sprintf("memory: use of %#x after it was resized", uintptr(p)))
	}
	panic(fmt.Sprintf("memory: use of %#x after it was freed", uintptr(p)))
}

func checkTrailer(p unsafe.Pointer, size int) {
	switch t := trailerOf(p, size); {
	case t.magic == movedMagic, t.magic == liveMagic && t.size != int64(size):
		panic(fmt.Sprintf("memory: use of %#x after it was resized", uintptr(p)))
	case t.magic != liveMagic:
		panic(fmt.Sprintf("memory: use of %#x after it was freed", uintptr(p)))
	}
}

type guard struct {
	busy atomic.Bool
}

func (g *guard) enter() {
	if !g.busy.CompareAndSwap(false, true) {
		panic("memory: Local used by several goroutines at once; use Shared")
	}
}

func (g *guard) exit() { g.busy.Store(false) }

func stampSlot(s *span, p unsafe.Pointer, size int) { *s.side(s.index(p)) = uint16(size) }

func verifySlot(s *span, p unsafe.Pointer, size int, owner uint32) {
	switch {
	case s.owner != owner:
		panic(fmt.Sprintf("memory: %#x belongs to a different allocator", uintptr(p)))
	case s.isBump():
		return
	}
	off := uint32(uintptr(p)&spanMask) - s.data
	i := s.index(p)
	if i >= s.slots || i*s.slot != off {
		panic(fmt.Sprintf("memory: %#x is not a live allocation: already freed, or never allocated here", uintptr(p)))
	}
	switch got := *s.side(i); {
	case got == 0:
		panic(fmt.Sprintf("memory: %#x is already freed", uintptr(p)))
	case got != uint16(size):
		panic(fmt.Sprintf("memory: %#x was resized since this handle was made", uintptr(p)))
	}
}

func eraseSlot(s *span, p unsafe.Pointer) { *s.side(s.index(p)) = 0 }

func stampTrailer(p unsafe.Pointer, size int) {
	*trailerOf(p, size) = trailer{magic: liveMagic, size: int64(size)}
}

func eraseTrailer(p unsafe.Pointer, size int) { trailerOf(p, size).magic = freedMagic }

func supersedeTrailer(p unsafe.Pointer, size int) { trailerOf(p, size).magic = movedMagic }
