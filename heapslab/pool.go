package heapslab

import (
	"fmt"
	"reflect"

	"github.com/stubbedev/memory-go/internal/slot"
)

// Item is a T owned by a Pool. The zero Item is nil.
type Item[T any] struct {
	s *slot.Slot[T]
}

// Value returns a pointer to the T, valid until the Item is put back or its
// pool is reset.
func (it Item[T]) Value() *T {
	if !it.s.Live() {
		slot.UseAfterPut("heapslab", it.s)
	}
	return &it.s.Value
}

// IsNil reports whether it refers to nothing.
func (it Item[T]) IsNil() bool { return it.s == nil }

// Pool hands out zeroed Ts from Go-heap chunks and takes them back for reuse.
// It is not safe for concurrent use.
type Pool[T any] struct {
	c    heapChunks[slot.Slot[T]]
	free []*slot.Slot[T]
	id   uint64
	n    int
}

// NewPool returns a Pool that grows by chunkSize values at a time. It allocates
// the first chunk immediately.
func NewPool[T any](chunkSize int) (*Pool[T], error) {
	if reflect.TypeFor[T]().Size() == 0 {
		return nil, fmt.Errorf("heapslab: %s: %w", reflect.TypeFor[T](), ErrZeroSize)
	}
	c, err := newHeapChunks[slot.Slot[T]](chunkSize)
	if err != nil {
		return nil, err
	}
	return &Pool[T]{c: c, id: slot.NewID()}, nil
}

// Len returns the number of Items handed out and not yet put back.
func (p *Pool[T]) Len() int { return p.n }

// Get returns a zeroed T.
func (p *Pool[T]) Get() Item[T] {
	var s *slot.Slot[T]
	if k := len(p.free) - 1; k >= 0 {
		s = p.free[k]
		p.free = p.free[:k]
	} else {
		s = p.c.take()
	}
	s.Mark(p.id)
	p.n++
	return Item[T]{s: s}
}

// Put zeroes it, so that nothing it referenced is kept alive by p, and
// returns it to p, invalidating it.
func (p *Pool[T]) Put(it Item[T]) {
	if it.s == nil {
		panic("heapslab: Put of the zero Item")
	}
	if !it.s.Unmark(p.id) {
		slot.NotLive("heapslab", it.s)
	}
	var zero T
	it.s.Value = zero
	p.free = append(p.free, it.s)
	p.n--
}

// Reset zeroes every value and invalidates every Item, keeping the chunks for
// reuse.
func (p *Pool[T]) Reset() {
	p.c.reset()
	clear(p.free)
	p.free, p.n = p.free[:0], 0
}

// Free zeroes every value, invalidates every Item and drops the chunks for the
// garbage collector. p must not be used afterwards.
func (p *Pool[T]) Free() {
	p.c.free()
	p.free, p.n = nil, 0
}
