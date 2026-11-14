package memory

import "unsafe"

func guardedReserve(size, align int) (unsafe.Pointer, error) { return osReserve(size, align) }

func guardedCommit(p unsafe.Pointer, size int) error { return osRecommit(p, size) }

func guardedRevoke(p unsafe.Pointer, size int) error { return osDecommit(p, size) }
