//go:build unix && !linux

package memory

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func osDecommit(p unsafe.Pointer, size int) error {
	_, err := unix.MmapPtr(-1, 0, p, uintptr(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON|unix.MAP_FIXED)
	return err
}
