//go:build memory_unchecked

package memory

import "unsafe"

// Checked reports whether the misuse guards are compiled in, which they are
// unless the build sets the memory_unchecked tag.
const Checked = false

const headerSize = 0

func checkLive(unsafe.Pointer) {}

type guard struct{}

func (guard) enter()                                   {}
func (guard) exit()                                    {}
func (guard) reset()                                   {}
func (guard) stamp(base unsafe.Pointer) unsafe.Pointer { return base }
func (guard) verify(p unsafe.Pointer) unsafe.Pointer   { return p }
