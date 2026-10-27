// Package pool provides a free-list allocator for a single type whose values
// live off the Go heap and are recycled individually.
//
// Get and Put never call the underlying allocator once a chunk has room, so
// they are the fastest way to allocate and free individual values in this
// module, and faster than new in a build with the memory_unchecked tag. With
// the guards compiled in, putting an Item twice, putting it into another pool,
// or reading it after Put panics.
package pool

import (
	"fmt"
	"sync/atomic"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/chunks"
)

// ErrChunkSize reports a chunk size that is not positive.
var ErrChunkSize = chunks.ErrSize

type slot[T any] struct {
	value T
	next  memory.Ref[slot[T]]
	tag   uint64
}

const live = 1

// Item is a T owned by a Pool. It may be stored off-heap. The zero Item is
// nil.
type Item[T any] struct {
	r memory.Ref[slot[T]]
}

// Value returns a Go pointer to the T, valid until the Item is put back.
func (it Item[T]) Value() *T {
	s := it.r.Value()
	if memory.Checked && s.tag&live == 0 {
		panic(fmt.Sprintf("pool: use of %p after Put", s))
	}
	return &s.value
}

// IsNil reports whether it refers to nothing.
func (it Item[T]) IsNil() bool { return it.r.IsNil() }

var ids atomic.Uint64

// Pool hands out zeroed Ts and takes them back for reuse. It is not safe for
// concurrent use.
type Pool[T any] struct {
	c    *chunks.Chunks[slot[T]]
	free memory.Ref[slot[T]]
	id   uint64
	n    int
}

// New returns a Pool that grows by chunkSize values at a time. It allocates
// the first chunk immediately.
func New[T any](a memory.Allocator, chunkSize int) (*Pool[T], error) {
	c, err := chunks.New[slot[T]](a, chunkSize)
	if err != nil {
		return nil, err
	}
	return &Pool[T]{c: c, id: ids.Add(1) << 1}, nil
}

// Len returns the number of Items handed out and not yet put back.
func (p *Pool[T]) Len() int { return p.n }

// Get returns a zeroed T.
func (p *Pool[T]) Get() (Item[T], error) {
	r := p.free
	if r.IsNil() {
		var err error
		if r, err = p.c.Take(); err != nil {
			return Item[T]{}, err
		}
	} else {
		p.free = r.Value().next
		*r.Value() = slot[T]{}
	}
	if memory.Checked {
		r.Value().tag = p.id | live
	}
	p.n++
	return Item[T]{r: r}, nil
}

// Put returns it to p, invalidating it.
func (p *Pool[T]) Put(it Item[T]) {
	s := it.r.Value()
	if memory.Checked {
		if s.tag != p.id|live {
			panic(fmt.Sprintf("pool: Put of %p, which is not live in this pool", s))
		}
		s.tag = p.id
	}
	s.next = p.free
	p.free = it.r
	p.n--
}

// Free releases every chunk, invalidating all Items. p must not be used
// afterwards.
func (p *Pool[T]) Free() error {
	p.free, p.n = memory.Ref[slot[T]]{}, 0
	return p.c.Free()
}
