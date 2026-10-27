//go:build memory_unchecked

package memory

import "unsafe"

// Checked reports whether the misuse guards are compiled in, which they are
// unless the build sets the memory_unchecked tag.
const Checked = false

const headerSize = 0

func checkLive(unsafe.Pointer, int) {}

type guard struct{}

func (guard) enter()                                          {}
func (guard) exit()                                           {}
func (guard) reset()                                          {}
func (guard) stamp(base unsafe.Pointer, _ int) unsafe.Pointer { return base }
func (guard) verify(unsafe.Pointer, int)                      {}
