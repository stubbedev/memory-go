// Package typedarena provides a chunked bump allocator for a single type whose
// values live off the Go heap and are freed all at once.
package typedarena

import (
	"errors"
	"fmt"

	memory "github.com/stubbedev/memory-go"
)

// ErrChunkSize reports a chunk size that is not positive.
var ErrChunkSize = errors.New("chunk size must be positive")

// Arena hands out zeroed Ts from chunks of chunkSize elements. It is not safe
// for concurrent use. Everything it hands out stays valid until Free.
type Arena[T any] struct {
	alloc     memory.Allocator
	chunkSize int
	chunks    []memory.Slice[T]
	cur       memory.Slice[T]
	used      int
}

// New returns an Arena whose chunks hold chunkSize elements. It allocates the
// first chunk immediately.
func New[T any](a memory.Allocator, chunkSize int) (*Arena[T], error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("typedarena: chunk size %d: %w", chunkSize, ErrChunkSize)
	}
	ta := &Arena[T]{alloc: a, chunkSize: chunkSize}
	if err := ta.refill(); err != nil {
		return nil, err
	}
	return ta, nil
}

func (ta *Arena[T]) chunk(n int) (memory.Slice[T], error) {
	c, err := memory.MakeSlice[T](ta.alloc, n)
	if err != nil {
		return memory.Slice[T]{}, err
	}
	ta.chunks = append(ta.chunks, c)
	return c, nil
}

func (ta *Arena[T]) refill() error {
	c, err := ta.chunk(ta.chunkSize)
	if err != nil {
		return err
	}
	ta.cur, ta.used = c, 0
	return nil
}

func (ta *Arena[T]) reserve(n int) error {
	if n <= ta.cur.Len()-ta.used {
		return nil
	}
	return ta.refill()
}

// Alloc returns a reference to a new zeroed T.
func (ta *Arena[T]) Alloc() (memory.Ref[T], error) {
	if err := ta.reserve(1); err != nil {
		return memory.Ref[T]{}, err
	}
	r := ta.cur.Ref(ta.used)
	ta.used++
	return r, nil
}

// AllocSlice returns n new zeroed Ts. Requests larger than the chunk size get
// a chunk of their own.
func (ta *Arena[T]) AllocSlice(n int) ([]T, error) {
	if n < 0 || n > ta.chunkSize {
		c, err := ta.chunk(n)
		return c.Elems(), err
	}
	if err := ta.reserve(n); err != nil {
		return nil, err
	}
	s := ta.cur.Elems()[ta.used : ta.used+n : ta.used+n]
	ta.used += n
	return s, nil
}

// Free releases every chunk, invalidating everything ta handed out. ta must not
// be used afterwards.
func (ta *Arena[T]) Free() error {
	errs := make([]error, 0, len(ta.chunks))
	for _, c := range ta.chunks {
		errs = append(errs, memory.FreeSlice(ta.alloc, c))
	}
	ta.chunks, ta.cur, ta.used = nil, memory.Slice[T]{}, 0
	return errors.Join(errs...)
}
