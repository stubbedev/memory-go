// Package heapslab provides a free-list Pool and a bump Arena for one type on
// the Go heap, for types that hold Go pointers and so cannot live off-heap.
//
// Values are carved from chunks, ordinary []T slices, so the garbage collector
// still scans them, but it tracks one object per chunk instead of one per
// value, neighbouring values share cache lines, and a batch is freed or reused
// in one step. Pool.Put, Pool.Reset and Arena.Reset zero what they take back,
// so whatever those values referenced becomes collectable at once.
//
// A pointer into a chunk keeps the whole chunk alive, so nothing handed out
// ever dangles: using a value after it was put back or reset reads or
// overwrites whatever the slot holds now, which is the one hazard. With the
// guards compiled in, a Pool catches putting an Item twice, putting it into
// another pool, and reading it after Put or Reset. An Arena checks nothing.
package heapslab

import (
	"fmt"

	"github.com/stubbedev/memory-go/internal/chunks"
)

var (
	// ErrChunkSize reports a chunk size that is not positive.
	ErrChunkSize = chunks.ErrSize
	// ErrZeroSize reports a type with no bytes to pool.
	ErrZeroSize = chunks.ErrZeroSize
)

type heapChunks[E any] struct {
	size int
	full [][]E
	big  [][]E
	cur  int
	used int
}

func newHeapChunks[E any](size int) (heapChunks[E], error) {
	if size <= 0 {
		return heapChunks[E]{}, fmt.Errorf("chunk size %d: %w", size, ErrChunkSize)
	}
	return heapChunks[E]{size: size, full: [][]E{make([]E, size)}}, nil
}

func (c *heapChunks[E]) reserve(n int) {
	if n <= c.size-c.used {
		return
	}
	c.cur++
	if c.cur == len(c.full) {
		c.full = append(c.full, make([]E, c.size))
	}
	c.used = 0
}

func (c *heapChunks[E]) take() *E {
	c.reserve(1)
	e := &c.full[c.cur][c.used]
	c.used++
	return e
}

func (c *heapChunks[E]) takeSlice(n int) []E {
	if n > c.size {
		s := make([]E, n)
		c.big = append(c.big, s)
		return s
	}
	c.reserve(n)
	s := c.full[c.cur][c.used : c.used+n : c.used+n]
	c.used += n
	return s
}

func (c *heapChunks[E]) reset() {
	if c.full == nil {
		return
	}
	for _, chunk := range c.full[:c.cur] {
		clear(chunk)
	}
	clear(c.full[c.cur][:c.used])
	for _, s := range c.big {
		clear(s)
	}
	clear(c.big)
	c.big = c.big[:0]
	c.cur, c.used = 0, 0
}

func (c *heapChunks[E]) free() {
	c.reset()
	c.full, c.big = nil, nil
}
