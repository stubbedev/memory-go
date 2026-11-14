// Package chunks carves single values and short runs of a type from
// fixed-size off-heap chunks, the storage behind typedarena, pool and intern.
package chunks

import (
	"errors"
	"fmt"

	memory "github.com/stubbedev/memory-go"
)

var (
	// ErrSize reports a chunk size that is not positive.
	ErrSize = errors.New("chunk size must be positive")
	// ErrZeroSize reports a type with no bytes to pool.
	ErrZeroSize = errors.New("pool of a zero-size type")
)

// Chunks hands out zeroed Ts from chunks of a fixed size. Runs longer than a
// chunk get a chunk of their own.
type Chunks[T any] struct {
	alloc memory.Allocator
	size  int
	full  []memory.Slice[T]
	big   []memory.Slice[T]
	elems []T
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
	return &Chunks[T]{alloc: a, size: size, full: []memory.Slice[T]{first}, elems: first.Elems()}, nil
}

func (c *Chunks[T]) reserve(n int) error {
	if n <= c.size-c.used {
		return nil
	}
	if c.cur+1 < len(c.full) {
		c.cur++
		c.elems = c.full[c.cur].Elems()
		clear(c.elems)
		c.used = 0
		return nil
	}
	next, err := memory.MakeSlice[T](c.alloc, c.size)
	if err != nil {
		return err
	}
	c.full = append(c.full, next)
	c.cur, c.used, c.elems = len(c.full)-1, 0, next.Elems()
	return nil
}

// Take returns a reference to a new zeroed T.
func (c *Chunks[T]) Take() (memory.Ref[T], error) {
	if c.used == c.size {
		if err := c.reserve(1); err != nil {
			return memory.Ref[T]{}, err
		}
	}
	r := c.full[c.cur].Ref(c.used)
	c.used++
	return r, nil
}

// TakeSlice returns n new zeroed Ts.
func (c *Chunks[T]) TakeSlice(n int) ([]T, error) {
	v, err := c.TakeView(n)
	if err != nil {
		return nil, err
	}
	return v.Elems(), nil
}

// TakeView returns n new zeroed Ts as a handle that may be stored off-heap.
func (c *Chunks[T]) TakeView(n int) (memory.View[T], error) {
	if n < 0 || n > c.size {
		s, err := memory.MakeSlice[T](c.alloc, n)
		if err != nil {
			return memory.View[T]{}, err
		}
		c.big = append(c.big, s)
		return s.View(0, n), nil
	}
	if err := c.reserve(n); err != nil {
		return memory.View[T]{}, err
	}
	v := c.full[c.cur].View(c.used, c.used+n)
	c.used += n
	return v, nil
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
	c.elems = c.full[0].Elems()
	clear(c.elems[:dirty])
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
	c.full, c.elems, c.cur, c.used = nil, nil, 0, 0
	return errors.Join(errs...)
}
