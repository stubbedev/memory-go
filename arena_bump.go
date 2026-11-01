package memory

import "unsafe"

func (s *span) isBump() bool { return s.slot == 0 }

func (s *span) holds(p unsafe.Pointer, size int) bool {
	off := uint32(uintptr(p) & spanMask)
	return off >= s.data && off+uint32(size) <= s.carved
}
