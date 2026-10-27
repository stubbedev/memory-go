// Package vector provides a growable slice whose elements live off the Go heap.
package vector

import (
	"iter"

	memory "github.com/stubbedev/memory-go"
)

// Vector is a growable sequence of off-heap Ts. It is not safe for concurrent
// use. Pointers and slices into it are valid until the next call that may
// grow or shrink it.
type Vector[T any] struct {
	alloc memory.Allocator
	buf   memory.Slice[T]
	n     int
}

// New returns an empty Vector with room for capacity elements.
func New[T any](a memory.Allocator, capacity int) (*Vector[T], error) {
	buf, err := memory.MakeSlice[T](a, capacity)
	if err != nil {
		return nil, err
	}
	return &Vector[T]{alloc: a, buf: buf}, nil
}

// Len returns the number of elements.
func (v *Vector[T]) Len() int { return v.n }

// Cap returns the number of elements v can hold without growing.
func (v *Vector[T]) Cap() int { return v.buf.Len() }

// Slice returns the elements as a Go slice.
func (v *Vector[T]) Slice() []T { return v.buf.Elems()[:v.n] }

// At returns element i. It panics if i is out of range.
func (v *Vector[T]) At(i int) T { return v.Slice()[i] }

// Ptr returns a pointer to element i. It panics if i is out of range.
func (v *Vector[T]) Ptr(i int) *T { return &v.Slice()[i] }

// Set replaces element i. It panics if i is out of range.
func (v *Vector[T]) Set(i int, value T) { v.Slice()[i] = value }

// Reserve grows v, if needed, to hold at least n elements.
func (v *Vector[T]) Reserve(n int) error {
	if n <= v.buf.Len() {
		return nil
	}
	return v.resize(max(n, 2*v.buf.Len(), 4))
}

// Clip shrinks the capacity of v to its length.
func (v *Vector[T]) Clip() error { return v.resize(v.n) }

func (v *Vector[T]) resize(n int) error {
	buf, err := memory.ResizeSlice(v.alloc, v.buf, n)
	if err != nil {
		return err
	}
	v.buf = buf
	return nil
}

// Push appends value.
func (v *Vector[T]) Push(value T) error {
	if err := v.Reserve(v.n + 1); err != nil {
		return err
	}
	v.buf.Elems()[v.n] = value
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
	return v.At(v.n - 1), true
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
	clear(v.buf.Elems()[n:v.n])
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

// Free releases the elements and leaves v empty.
func (v *Vector[T]) Free() error {
	err := memory.FreeSlice(v.alloc, v.buf)
	v.buf, v.n = memory.Slice[T]{}, 0
	return err
}
