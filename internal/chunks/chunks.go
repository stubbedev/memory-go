// Package chunks carves single values and short runs of a type from
// fixed-size off-heap chunks, the storage behind typedarena and pool.
package chunks

import (
	"errors"
	"fmt"

	memory "github.com/stubbedev/memory-go"
)

// ErrSize reports a chunk size that is not positive.
var ErrSize = errors.New("chunk size must be positive")

// Chunks hands out zeroed Ts from chunks of a fixed size. Runs longer than a
// chunk get a chunk of their own.
type Chunks[T any] struct {
	alloc memory.Allocator
	size  int
	full  []memory.Slice[T]
	big   []memory.Slice[T]
	cur   int
	used  int
}

// New returns Chunks of size elements each and allocates the first one.
func New[T any](a memory.Allocator, size int) (*Chunks[T], error) {
	if size <= 0 {
		return nil, fmt.Errorf("chunk size %d: %w", size, ErrSize)
	}
	first, err := memory.MakeSlice[T](a, size)
	if err != nil {
		return nil, err
	}
	return &Chunks[T]{alloc: a, size: size, full: []memory.Slice[T]{first}}, nil
}

func (c *Chunks[T]) reserve(n int) error {
	if n <= c.size-c.used {
		return nil
	}
	if c.cur+1 < len(c.full) {
		c.cur++
		clear(c.full[c.cur].Elems())
		c.used = 0
		return nil
	}
	next, err := memory.MakeSlice[T](c.alloc, c.size)
	if err != nil {
		return err
	}
	c.full = append(c.full, next)
	c.cur, c.used = len(c.full)-1, 0
	return nil
}

// Take returns a reference to a new zeroed T.
func (c *Chunks[T]) Take() (memory.Ref[T], error) {
	if err := c.reserve(1); err != nil {
		return memory.Ref[T]{}, err
	}
	r := c.full[c.cur].Ref(c.used)
	c.used++
	return r, nil
}

// TakeSlice returns n new zeroed Ts.
func (c *Chunks[T]) TakeSlice(n int) ([]T, error) {
	if n < 0 || n > c.size {
		s, err := memory.MakeSlice[T](c.alloc, n)
		if err != nil {
			return nil, err
		}
		c.big = append(c.big, s)
		return s.Elems(), nil
	}
	if err := c.reserve(n); err != nil {
		return nil, err
	}
	s := c.full[c.cur].Elems()[c.used : c.used+n : c.used+n]
	c.used += n
	return s, nil
}

func (c *Chunks[T]) freeBig() error {
	errs := make([]error, 0, len(c.big))
	for _, s := range c.big {
		errs = append(errs, memory.FreeSlice(c.alloc, s))
	}
	c.big = nil
	return errors.Join(errs...)
}

// Reset makes all chunk memory available again, zeroed, and frees the
// oversized runs.
func (c *Chunks[T]) Reset() error {
	dirty := c.size
	if c.cur == 0 {
		dirty = c.used
	}
	clear(c.full[0].Elems()[:dirty])
	c.cur, c.used = 0, 0
	return c.freeBig()
}

// Free releases every chunk. c must not be used afterwards.
func (c *Chunks[T]) Free() error {
	errs := make([]error, 0, len(c.full)+1)
	for _, s := range c.full {
		errs = append(errs, memory.FreeSlice(c.alloc, s))
	}
	errs = append(errs, c.freeBig())
	c.full, c.cur, c.used = nil, 0, 0
	return errors.Join(errs...)
}
