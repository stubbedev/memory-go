// Package minheap provides a binary heap whose elements live off the Go heap.
package minheap

import (
	"iter"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/vector"
)

// Heap pops the element for which less reports true against every other
// first; pass a greater-than func for a max-heap. It is not safe for
// concurrent use.
type Heap[T any] struct {
	items *vector.Vector[T]
	less  func(a, b T) bool
}

// New returns an empty Heap ordered by less.
func New[T any](a memory.Allocator, less func(a, b T) bool) (*Heap[T], error) {
	items, err := vector.New[T](a, 0)
	if err != nil {
		return nil, err
	}
	return &Heap[T]{items: items, less: less}, nil
}

// Len returns the number of elements.
func (h *Heap[T]) Len() int { return h.items.Len() }

// Push adds value.
func (h *Heap[T]) Push(value T) error {
	if err := h.items.Push(value); err != nil {
		return err
	}
	h.up(h.items.Len() - 1)
	return nil
}

// Peek returns the least element without removing it, or reports false if h
// is empty.
func (h *Heap[T]) Peek() (T, bool) {
	if h.items.Len() == 0 {
		var zero T
		return zero, false
	}
	return h.items.At(0), true
}

// Pop removes and returns the least element, or reports false if h is empty.
func (h *Heap[T]) Pop() (T, bool) {
	s := h.items.Slice()
	if len(s) == 0 {
		var zero T
		return zero, false
	}
	last := len(s) - 1
	s[0], s[last] = s[last], s[0]
	value, _ := h.items.Pop()
	h.down(0)
	return value, true
}

// DeleteFunc removes every element for which del returns true and returns how
// many it removed.
func (h *Heap[T]) DeleteFunc(del func(T) bool) int {
	removed := h.items.DeleteFunc(del)
	for i := h.items.Len()/2 - 1; i >= 0; i-- {
		h.down(i)
	}
	return removed
}

// All yields every element in heap order, which is not sorted order.
func (h *Heap[T]) All() iter.Seq2[int, T] { return h.items.All() }

// Clip shrinks the capacity of h to its length.
func (h *Heap[T]) Clip() error { return h.items.Clip() }

// Free releases the elements and leaves h empty.
func (h *Heap[T]) Free() error { return h.items.Free() }

func (h *Heap[T]) up(i int) {
	s := h.items.Slice()
	for i > 0 {
		parent := (i - 1) / 2
		if !h.less(s[i], s[parent]) {
			return
		}
		s[i], s[parent] = s[parent], s[i]
		i = parent
	}
}

func (h *Heap[T]) down(i int) {
	s := h.items.Slice()
	for {
		least := i
		for _, child := range [2]int{2*i + 1, 2*i + 2} {
			if child < len(s) && h.less(s[child], s[least]) {
				least = child
			}
		}
		if least == i {
			return
		}
		s[i], s[least] = s[least], s[i]
		i = least
	}
}
