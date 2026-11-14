// Package pool provides a free-list allocator for a single type whose values
// live off the Go heap and are recycled individually.
//
// Get and Put never call the underlying allocator once a chunk has room, so
// they are the fastest way to allocate and free individual values in this
// module, and faster than new. The free list runs through the unused values
// themselves, so a pool adds no per-value overhead beyond rounding small types
// up to eight bytes. With the guards compiled in, each value also carries an
// 8-byte tag, and putting an Item twice, putting it into another pool, or reading
// it after Put panics.
package pool

import (
	"fmt"
	"reflect"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/chunks"
	"github.com/stubbedev/memory-go/internal/slot"
)

var (
	// ErrChunkSize reports a chunk size that is not positive.
	ErrChunkSize = chunks.ErrSize
	// ErrZeroSize reports a type with no bytes to pool.
	ErrZeroSize = chunks.ErrZeroSize
)

// Item is a T owned by a Pool. It may be stored off-heap. The zero Item is
// nil.
type Item[T any] struct {
	r memory.Ref[slot.Slot[T]]
}

// Value returns a Go pointer to the T, valid until the Item is put back.
func (it Item[T]) Value() *T {
	s := it.r.Value()
	if !s.Live() {
		slot.UseAfterPut("pool", s)
	}
	return &s.Value
}

// IsNil reports whether it refers to nothing.
func (it Item[T]) IsNil() bool { return it.r.IsNil() }

// Pool hands out zeroed Ts and takes them back for reuse. It is not safe for
// concurrent use.
type Pool[T any] struct {
	c    *chunks.Chunks[slot.Slot[T]]
	free memory.Ref[slot.Slot[T]]
	id   uint64
	n    int
}

// New returns a Pool that grows by chunkSize values at a time. It allocates
// the first chunk immediately.
func New[T any](a memory.Allocator, chunkSize int) (*Pool[T], error) {
	var zero T
	if unsafe.Sizeof(zero) == 0 {
		return nil, fmt.Errorf("pool: %s: %w", reflect.TypeFor[T](), ErrZeroSize)
	}
	c, err := chunks.New[slot.Slot[T]](a, chunkSize)
	if err != nil {
		return nil, err
	}
	return &Pool[T]{c: c, id: slot.NewID()}, nil
}

// Len returns the number of Items handed out and not yet put back.
func (p *Pool[T]) Len() int { return p.n }

func nextOf[T any](s *slot.Slot[T]) *memory.Ref[slot.Slot[T]] {
	return (*memory.Ref[slot.Slot[T]])(unsafe.Pointer(&s.Value))
}

// Get returns a zeroed T.
func (p *Pool[T]) Get() (Item[T], error) {
	r, recycled, err := p.take()
	if err != nil {
		return Item[T]{}, err
	}
	if recycled {
		*r.Value() = slot.Slot[T]{}
	}
	r.Value().Mark(p.id)
	p.n++
	return Item[T]{r: r}, nil
}

// GetUninit is Get without zeroing a recycled value, which then holds what it
// held when it was put back, with its first eight bytes overwritten. Use it only
// for a value that is written in full before it is read.
func (p *Pool[T]) GetUninit() (Item[T], error) {
	r, _, err := p.take()
	if err != nil {
		return Item[T]{}, err
	}
	r.Value().Mark(p.id)
	p.n++
	return Item[T]{r: r}, nil
}

func (p *Pool[T]) take() (memory.Ref[slot.Slot[T]], bool, error) {
	r := p.free
	if r.IsNil() {
		fresh, err := p.c.Take()
		return fresh, false, err
	}
	p.free = *nextOf(r.Value())
	return r, true, nil
}

// Put returns it to p, invalidating it.
func (p *Pool[T]) Put(it Item[T]) {
	if it.r.IsNil() {
		panic("pool: Put of the zero Item")
	}
	s := it.r.Value()
	if !s.Unmark(p.id) {
		slot.NotLive("pool", s)
	}
	*(*uintptr)(unsafe.Pointer(nextOf(s))) = uintptr(unsafe.Pointer(p.free.Value()))
	p.free = it.r
	p.n--
}

// Free releases every chunk, invalidating all Items. p must not be used
// afterwards.
func (p *Pool[T]) Free() error {
	p.free, p.n = memory.Ref[slot.Slot[T]]{}, 0
	return p.c.Free()
}
