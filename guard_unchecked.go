//go:build memory_unchecked

package memory

import "unsafe"

// Checked reports whether the misuse guards are compiled in, which they are
// unless the build sets the memory_unchecked tag.
const Checked = false

const (
	sideEntry   = 0
	trailerSize = 0
)

func trailerOffset(size int) int { return size }

func checkLive(unsafe.Pointer, int) {}

type guard struct{}

func (guard) enter() {}
func (guard) exit()  {}

func stampSlot(*span, unsafe.Pointer, int)          {}
func verifySlot(*span, unsafe.Pointer, int, uint32) {}
func eraseSlot(*span, unsafe.Pointer)               {}
func stampTrailer(unsafe.Pointer, int)              {}
func eraseTrailer(unsafe.Pointer, int)              {}
func supersedeTrailer(unsafe.Pointer, int)          {}
