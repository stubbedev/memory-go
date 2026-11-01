package memory

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"
)

func osDecommit(p unsafe.Pointer, size int) error {
	return unix.Madvise(unsafe.Slice((*byte)(p), size), unix.MADV_DONTNEED)
}

func osGrow(p unsafe.Pointer, size, to int) (unsafe.Pointer, bool, error) {
	moved, err := unix.MremapPtr(p, uintptr(size), nil, uintptr(to), unix.MREMAP_MAYMOVE)
	if err != nil {
		return nil, false, err
	}
	return moved, true, nil
}

func osAdviseHuge(p unsafe.Pointer, size int) error {
	err := unix.Madvise(unsafe.Slice((*byte)(p), size), unix.MADV_HUGEPAGE)
	if errors.Is(err, unix.EINVAL) {
		return nil
	}
	return err
}
