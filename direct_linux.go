package memory

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func osDecommit(p unsafe.Pointer, size int) error {
	return unix.Madvise(unsafe.Slice((*byte)(p), size), unix.MADV_DONTNEED)
}
