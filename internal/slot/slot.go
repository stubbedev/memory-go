// Package slot holds the values of a free-list allocator and marks them live or
// free, so that checked builds catch a value put back twice, put into another
// pool, or used after it was put back. With memory_unchecked a Slot holds only
// its value, is always live, and marking it does nothing.
package slot

import (
	"fmt"
	"sync/atomic"
)

var ids atomic.Uint64

// NewID returns a pool id for Mark and Unmark, unique within the process.
func NewID() uint64 { return ids.Add(1) << 1 }

// UseAfterPut panics, naming the package pkg and the slot s read after Put.
func UseAfterPut(pkg string, s any) {
	panic(fmt.Sprintf("%s: use of %p after Put", pkg, s))
}

// NotLive panics, naming the package pkg and the slot s put back while not
// live in that pool.
func NotLive(pkg string, s any) {
	panic(fmt.Sprintf("%s: Put of %p, which is not live in this pool", pkg, s))
}
