//go:build unix

package memory

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"
)

func guardedReserve(size, align int) (unsafe.Pointer, error) {
	base, err := osReserve(size, align)
	if err != nil {
		return nil, err
	}
	if err := guardedRevoke(base, size); err != nil {
		return nil, errors.Join(err, osUnmap(base, size))
	}
	return base, nil
}

func guardedCommit(p unsafe.Pointer, size int) error {
	return unix.Mprotect(unsafe.Slice((*byte)(p), size), unix.PROT_READ|unix.PROT_WRITE)
}

func guardedRevoke(p unsafe.Pointer, size int) error {
	_, err := unix.MmapPtr(-1, 0, p, uintptr(size), unix.PROT_NONE, unix.MAP_PRIVATE|unix.MAP_ANON|unix.MAP_FIXED)
	return err
}
