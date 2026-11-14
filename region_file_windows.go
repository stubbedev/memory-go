package memory

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func mapRegionFile(f *os.File, size int) (unsafe.Pointer, uintptr, error) {
	h := windows.Handle(f.Fd())
	var returned uint32
	_ = windows.DeviceIoControl(h, windows.FSCTL_SET_SPARSE, nil, 0, nil, 0, &returned, nil)
	section, err := windows.CreateFileMapping(h, nil, windows.PAGE_READWRITE, uint32(uint64(size)>>32), uint32(size), nil)
	if err != nil {
		return nil, 0, err
	}
	addr, err := windows.MapViewOfFile(section, windows.FILE_MAP_WRITE, 0, 0, uintptr(size))
	if err != nil {
		return nil, 0, errors.Join(err, windows.CloseHandle(section))
	}
	return pointerAt(addr), uintptr(section), nil
}

func unmapRegionFile(rf *regionFile, p unsafe.Pointer, _, keep int) error {
	err := errors.Join(windows.UnmapViewOfFile(uintptr(p)), windows.CloseHandle(windows.Handle(rf.section)))
	if terr := rf.f.Truncate(int64(keep)); terr != nil && !errors.Is(terr, windows.ERROR_USER_MAPPED_FILE) {
		err = errors.Join(err, terr)
	}
	return err
}

func growRegionFile(*os.File, int64) error { return nil }

func shrinkRegionFile(_ *os.File, base unsafe.Pointer, boundary, to int) error {
	clearFrom(base, boundary, to)
	return nil
}

func syncRegionFile(p unsafe.Pointer, size int) error {
	return windows.FlushViewOfFile(uintptr(p), uintptr(size))
}
