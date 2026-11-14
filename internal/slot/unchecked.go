//go:build memory_unchecked

package slot

// Slot is a T together with the pool it is live in, if any.
type Slot[T any] struct {
	_     [0]uint64
	Value T
}

// Live reports whether s is live.
func (*Slot[T]) Live() bool { return true }

// Mark makes s live in the pool id.
func (*Slot[T]) Mark(uint64) {}

// Unmark makes s free in the pool id if it was live there, and reports whether
// it was.
func (*Slot[T]) Unmark(uint64) bool { return true }
