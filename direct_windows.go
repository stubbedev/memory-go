package memory

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func osMap(size int) (unsafe.Pointer, error) {
	addr, err := windows.VirtualAlloc(0, uintptr(size), windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		return nil, err
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr)), nil
}

func osUnmap(p unsafe.Pointer, _ int) error {
	return windows.VirtualFree(uintptr(p), 0, windows.MEM_RELEASE)
}

func osDecommit(p unsafe.Pointer, size int) error {
	return windows.VirtualFree(uintptr(p), uintptr(size), windows.MEM_DECOMMIT)
}

func osRecommit(p unsafe.Pointer, size int) error {
	_, err := windows.VirtualAlloc(uintptr(p), uintptr(size), windows.MEM_COMMIT, windows.PAGE_READWRITE)
	return err
}
