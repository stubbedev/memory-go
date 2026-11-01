package memory

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

func pageSpan(p unsafe.Pointer, size int) (unsafe.Pointer, int) {
	lead := int(uintptr(p) & uintptr(osPageSize-1))
	return unsafe.Add(p, -lead), (lead + size + osPageSize - 1) &^ (osPageSize - 1)
}

func touchPages(p unsafe.Pointer, size int) {
	for off := 0; off < size; off += osPageSize {
		atomic.AddUint32((*uint32)(unsafe.Add(p, off)), 0)
	}
}

func populate(p unsafe.Pointer, size int) error {
	start, length := pageSpan(p, size)
	if err := osPopulate(start, length); err != nil {
		return fmt.Errorf("memory: populate %d bytes at %#x: %w", length, uintptr(start), err)
	}
	return nil
}

// Populate makes s resident now, in one batch, so that first use takes no page
// faults: a fault costs far more than the access it interrupts. Contents do not
// change. Use it before a latency-sensitive section, knowing RSS rises at once. It
// panics if s is already freed.
func Populate[T any](s Slice[T]) error {
	if s.n == 0 {
		return nil
	}
	s.Elems()
	return populate(unsafe.Pointer(s.p), sizeOf[T](s.n))
}

// Populate makes the current elements of r resident now, like the function
// Populate.
func (r *Region[T]) Populate() error {
	if r.n == 0 {
		return nil
	}
	return populate(unsafe.Pointer(r.base), sizeOf[T](r.n))
}
