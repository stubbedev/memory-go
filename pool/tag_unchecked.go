//go:build memory_unchecked

package pool

import "unsafe"

type tag struct{}

func (*tag) checkLive(unsafe.Pointer)      {}
func (*tag) mark(uint64)                   {}
func (*tag) unmark(unsafe.Pointer, uint64) {}
