//go:build unix

package memory

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

func mapRegionFile(f *os.File, size int) (unsafe.Pointer, uintptr, error) {
	p, err := unix.MmapPtr(int(f.Fd()), 0, nil, uintptr(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	return p, 0, err
}

func unmapRegionFile(_ *regionFile, p unsafe.Pointer, size, _ int) error {
	return unix.MunmapPtr(p, uintptr(size))
}

func growRegionFile(f *os.File, size int64) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() >= size {
		return nil
	}
	return f.Truncate(size)
}

func shrinkRegionFile(f *os.File, _ unsafe.Pointer, boundary, _ int) error {
	return f.Truncate(int64(regionHeaderSize + boundary))
}

func syncRegionFile(p unsafe.Pointer, size int) error {
	return unix.Msync(unsafe.Slice((*byte)(p), size), unix.MS_SYNC)
}
