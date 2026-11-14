//go:build unix

package memory

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func osMap(size int) (unsafe.Pointer, error) {
	return unix.MmapPtr(-1, 0, nil, uintptr(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
}

func osReserve(size, align int) (unsafe.Pointer, error) {
	raw, err := osMap(size + align)
	if err != nil {
		return nil, err
	}
	lead := -int(uintptr(raw)) & (align - 1)
	if lead > 0 {
		if err := unix.MunmapPtr(raw, uintptr(lead)); err != nil {
			return nil, fmt.Errorf("trim leading %d bytes: %w", lead, err)
		}
	}
	base := unsafe.Add(raw, lead)
	if tail := align - lead; tail > 0 {
		if err := unix.MunmapPtr(unsafe.Add(base, size), uintptr(tail)); err != nil {
			return nil, fmt.Errorf("trim trailing %d bytes: %w", tail, err)
		}
	}
	return base, nil
}

func osUnmap(p unsafe.Pointer, size int) error { return unix.MunmapPtr(p, uintptr(size)) }

func osRecommit(unsafe.Pointer, int) error { return nil }

func osProtect(p unsafe.Pointer, size int, writable bool) error {
	prot := unix.PROT_READ
	if writable {
		prot |= unix.PROT_WRITE
	}
	return unix.Mprotect(unsafe.Slice((*byte)(p), size), prot)
}

func osShrink(p unsafe.Pointer, size, to int) (bool, error) {
	if err := unix.MunmapPtr(unsafe.Add(p, to), uintptr(size-to)); err != nil {
		return false, err
	}
	return true, nil
}
