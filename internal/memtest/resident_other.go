//go:build !linux

package memtest

// Resident returns the process's resident memory in bytes, or 0 where that is
// unavailable.
func Resident() int64 { return 0 }
