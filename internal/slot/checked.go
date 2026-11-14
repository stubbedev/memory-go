//go:build !memory_unchecked

package slot

const live = 1

// Slot is a T together with the pool it is live in, if any.
type Slot[T any] struct {
	_     [0]uint64
	word  uint64
	Value T
}

// Live reports whether s is live.
func (s *Slot[T]) Live() bool { return s.word&live != 0 }

// Mark makes s live in the pool id.
func (s *Slot[T]) Mark(id uint64) { s.word = id | live }

// Unmark makes s free in the pool id if it was live there, and reports whether
// it was.
func (s *Slot[T]) Unmark(id uint64) bool {
	if s.word != id|live {
		return false
	}
	s.word = id
	return true
}
