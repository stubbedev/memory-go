package memory

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const reserveAttempts = 16

func pointerAt(addr uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

func osMap(size int) (unsafe.Pointer, error) {
	addr, err := windows.VirtualAlloc(0, uintptr(size), windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
	if err != nil {
		return nil, err
	}
	return pointerAt(addr), nil
}

func osReserve(size, align int) (unsafe.Pointer, error) {
	var lastErr error
	for range reserveAttempts {
		probe, err := windows.VirtualAlloc(0, uintptr(size+align), windows.MEM_RESERVE, windows.PAGE_NOACCESS)
		if err != nil {
			return nil, err
		}
		if err := windows.VirtualFree(probe, 0, windows.MEM_RELEASE); err != nil {
			return nil, fmt.Errorf("release probe: %w", err)
		}
		aligned := (probe + uintptr(align) - 1) &^ (uintptr(align) - 1)
		addr, err := windows.VirtualAlloc(aligned, uintptr(size), windows.MEM_RESERVE, windows.PAGE_READWRITE)
		if err == nil {
			return pointerAt(addr), nil
		}
		lastErr = err
	}
	return nil, errors.Join(errors.New("no aligned address range stayed free"), lastErr)
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

func osShrink(unsafe.Pointer, int, int) (bool, error) { return false, nil }

func osGrow(unsafe.Pointer, int, int) (unsafe.Pointer, bool, error) { return nil, false, nil }

func osAdviseHuge(unsafe.Pointer, int) error { return nil }

func osPopulate(p unsafe.Pointer, size int) error {
	touchPages(p, size)
	return nil
}
