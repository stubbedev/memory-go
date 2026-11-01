// Package vector provides a growable slice whose elements live off the Go heap.
package vector

import (
	"iter"

	memory "github.com/stubbedev/memory-go"
)

// Vector is a growable sequence of off-heap Ts. It is not safe for concurrent
// use. Pointers and slices into it are valid until the next call that may
// grow or shrink it, except on a Vector from NewStable.
type Vector[T any] struct {
	alloc  memory.Allocator
	region *memory.Region[T]
	buf    memory.Slice[T]
	elems  []T
	n      int
}

// New returns an empty Vector with room for capacity elements.
func New[T any](a memory.Allocator, capacity int) (*Vector[T], error) {
	buf, err := memory.MakeSlice[T](a, capacity)
	if err != nil {
		return nil, err
	}
	return &Vector[T]{alloc: a, buf: buf, elems: buf.Elems()}, nil
}

// NewStable returns an empty Vector that never moves its elements, so pointers
// into it stay valid as it grows, and growing never copies. It reserves address
// space for maxLen elements, using memory only for those it holds, and growing
// past maxLen returns memory.ErrRegionFull. maxLen may be much larger than the
// Vector is likely to get.
func NewStable[T any](maxLen int) (*Vector[T], error) {
	region, err := memory.NewRegion[T](maxLen)
	if err != nil {
		return nil, err
	}
	return &Vector[T]{region: region}, nil
}

// Len returns the number of elements.
func (v *Vector[T]) Len() int { return v.n }

// Cap returns the number of elements v can hold without growing.
func (v *Vector[T]) Cap() int { return len(v.elems) }

// Slice returns the elements as a Go slice.
func (v *Vector[T]) Slice() []T { return v.elems[:v.n] }

// At returns element i. It panics if i is out of range.
func (v *Vector[T]) At(i int) T { return v.elems[:v.n][i] }

// Ptr returns a pointer to element i. It panics if i is out of range.
func (v *Vector[T]) Ptr(i int) *T { return &v.elems[:v.n][i] }

// Set replaces element i. It panics if i is out of range.
func (v *Vector[T]) Set(i int, value T) { v.elems[:v.n][i] = value }

// Reserve grows v, if needed, to hold at least n elements.
func (v *Vector[T]) Reserve(n int) error {
	if n <= len(v.elems) {
		return nil
	}
	return v.resize(max(n, 2*len(v.elems), 4))
}

// Clip shrinks the capacity of v to its length.
func (v *Vector[T]) Clip() error { return v.resize(v.n) }

func (v *Vector[T]) resize(n int) error {
	if v.region != nil {
		return v.resizeRegion(n)
	}
	buf, err := memory.ResizeSlice(v.alloc, v.buf, n)
	if err != nil {
		return err
	}
	v.buf = buf
	v.elems = buf.Elems()
	return nil
}

func (v *Vector[T]) resizeRegion(n int) error {
	limit := v.region.Cap()
	if n > limit && v.n < limit && len(v.elems) < limit {
		n = limit
	}
	if have := v.region.Len(); n > have {
		if _, err := v.region.Grow(n - have); err != nil {
			return err
		}
	} else if err := v.region.Truncate(n); err != nil {
		return err
	}
	v.elems = v.region.Elems()
	return nil
}

// Push appends value.
func (v *Vector[T]) Push(value T) error {
	if v.n == len(v.elems) {
		if err := v.Reserve(v.n + 1); err != nil {
			return err
		}
	}
	v.elems[v.n] = value
	v.n++
	return nil
}

// Pop removes and returns the last element, or reports false if v is empty.
func (v *Vector[T]) Pop() (T, bool) {
	value, ok := v.Last()
	if ok {
		v.Truncate(v.n - 1)
	}
	return value, ok
}

// Last returns the last element, or reports false if v is empty.
func (v *Vector[T]) Last() (T, bool) {
	if v.n == 0 {
		var zero T
		return zero, false
	}
	return v.elems[v.n-1], true
}

// RemoveAt removes element i, shifting later elements down, and returns it. It
// panics if i is out of range.
func (v *Vector[T]) RemoveAt(i int) T {
	s := v.Slice()
	value := s[i]
	copy(s[i:], s[i+1:])
	v.Truncate(v.n - 1)
	return value
}

// DeleteFunc removes every element for which del returns true, keeping the
// order of the rest, and returns how many it removed.
func (v *Vector[T]) DeleteFunc(del func(T) bool) int {
	s := v.Slice()
	kept := 0
	for _, value := range s {
		if !del(value) {
			s[kept] = value
			kept++
		}
	}
	removed := v.n - kept
	v.Truncate(kept)
	return removed
}

// Truncate shortens v to n elements. It panics if n is out of range.
func (v *Vector[T]) Truncate(n int) {
	clear(v.elems[n:v.n])
	v.n = n
}

// All yields each index and element in order.
func (v *Vector[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i := range v.n {
			if !yield(i, v.At(i)) {
				return
			}
		}
	}
}

// Free releases the elements and leaves v empty. A Vector from NewStable must
// not be used afterwards.
func (v *Vector[T]) Free() error {
	v.elems, v.n = nil, 0
	if v.region != nil {
		return v.region.Close()
	}
	err := memory.FreeSlice(v.alloc, v.buf)
	v.buf = memory.Slice[T]{}
	return err
}

// Append adds every value with one capacity check and one copy, which beats
// calling Push in a loop by a wide margin on large inputs.
func (v *Vector[T]) Append(values ...T) error {
	if err := v.Reserve(v.n + len(values)); err != nil {
		return err
	}
	copy(v.elems[v.n:], values)
	v.n += len(values)
	return nil
}
