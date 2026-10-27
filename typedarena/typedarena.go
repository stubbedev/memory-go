// Package typedarena provides a chunked bump allocator for a single type whose
// values live off the Go heap and are freed all at once.
//
// Allocation is a bounds check and an increment. Reset rewinds the arena so
// the same warm memory serves the next batch, which is where it outruns the
// Go allocator.
package typedarena

import (
	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/chunks"
)

// ErrChunkSize reports a chunk size that is not positive.
var ErrChunkSize = chunks.ErrSize

// Arena hands out zeroed Ts from chunks of chunkSize elements. It is not safe
// for concurrent use. Everything it hands out stays valid until Reset or Free.
type Arena[T any] struct {
	c *chunks.Chunks[T]
}

// New returns an Arena whose chunks hold chunkSize elements. It allocates the
// first chunk immediately.
func New[T any](a memory.Allocator, chunkSize int) (*Arena[T], error) {
	c, err := chunks.New[T](a, chunkSize)
	if err != nil {
		return nil, err
	}
	return &Arena[T]{c: c}, nil
}

// Alloc returns a reference to a new zeroed T.
func (ta *Arena[T]) Alloc() (memory.Ref[T], error) { return ta.c.Take() }

// AllocSlice returns n new zeroed Ts. Requests larger than the chunk size get
// a chunk of their own, which Reset frees.
func (ta *Arena[T]) AllocSlice(n int) ([]T, error) { return ta.c.TakeSlice(n) }

// Reset invalidates everything ta handed out and makes its chunks available
// again, zeroed, without returning them to the allocator.
func (ta *Arena[T]) Reset() error { return ta.c.Reset() }

// Free releases every chunk, invalidating everything ta handed out. ta must not
// be used afterwards.
func (ta *Arena[T]) Free() error { return ta.c.Free() }
