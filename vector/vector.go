// Package vector provides a growable slice whose elements live off the Go heap.
package vector

import (
	"iter"

	memory "github.com/stubbedev/memory-go"
)

type Vector[T any] struct {
	alloc memory.Allocator
	buf   []T
	n     int
}

func New[T any](a memory.Allocator, capacity int) (*Vector[T], error) {
	buf, err := memory.MakeSlice[T](a, capacity)
	if err != nil {
		return nil, err
	}
	return &Vector[T]{alloc: a, buf: buf}, nil
}

func (v *Vector[T]) Len() int { return v.n }

func (v *Vector[T]) Cap() int { return len(v.buf) }

func (v *Vector[T]) Slice() []T { return v.buf[:v.n] }

func (v *Vector[T]) At(i int) T { return v.Slice()[i] }

func (v *Vector[T]) Ptr(i int) *T { return &v.Slice()[i] }

func (v *Vector[T]) Set(i int, value T) { v.Slice()[i] = value }

func (v *Vector[T]) Reserve(n int) error {
	if n <= len(v.buf) {
		return nil
	}
	buf, err := memory.ResizeSlice(v.alloc, v.buf, max(n, 2*len(v.buf), 4))
	if err != nil {
		return err
	}
	v.buf = buf
	return nil
}

func (v *Vector[T]) Push(value T) error {
	if err := v.Reserve(v.n + 1); err != nil {
		return err
	}
	v.buf[v.n] = value
	v.n++
	return nil
}

func (v *Vector[T]) Pop() (T, bool) {
	value, ok := v.Last()
	if ok {
		v.Truncate(v.n - 1)
	}
	return value, ok
}

func (v *Vector[T]) Last() (T, bool) {
	if v.n == 0 {
		var zero T
		return zero, false
	}
	return v.buf[v.n-1], true
}

func (v *Vector[T]) RemoveAt(i int) T {
	s := v.Slice()
	value := s[i]
	copy(s[i:], s[i+1:])
	v.Truncate(v.n - 1)
	return value
}

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

func (v *Vector[T]) Truncate(n int) {
	clear(v.buf[n:v.n])
	v.n = n
}

func (v *Vector[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i := range v.n {
			if !yield(i, v.buf[i]) {
				return
			}
		}
	}
}

func (v *Vector[T]) Free() error {
	err := memory.FreeSlice(v.alloc, v.buf)
	v.buf, v.n = nil, 0
	return err
}
