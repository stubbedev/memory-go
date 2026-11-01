package memtest

import (
	"fmt"
	"os"
)

// Resident returns the process's resident memory in bytes, or 0 where that is
// unavailable.
func Resident() int64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	var size, pages int64
	if _, err := fmt.Sscan(string(raw), &size, &pages); err != nil {
		return 0
	}
	return pages * int64(os.Getpagesize())
}
