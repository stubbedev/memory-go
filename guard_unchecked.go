//go:build memory_unchecked

package memory

import "unsafe"

const headerSize = 0

func checkLive(unsafe.Pointer) {}

type guard struct{}

func (guard) enter()                                   {}
func (guard) exit()                                    {}
func (guard) reset()                                   {}
func (guard) stamp(base unsafe.Pointer) unsafe.Pointer { return base }
func (guard) verify(p unsafe.Pointer) unsafe.Pointer   { return p }
