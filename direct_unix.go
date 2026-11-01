//go:build unix

package memory

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func osMap(size int) (unsafe.Pointer, error) {
	return unix.MmapPtr(-1, 0, nil, uintptr(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
}

func osUnmap(p unsafe.Pointer, size int) error { return unix.MunmapPtr(p, uintptr(size)) }

func osRecommit(unsafe.Pointer, int) error { return nil }
