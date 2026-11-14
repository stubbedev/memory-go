package heapslab

import "fmt"

// Arena hands out zeroed Ts from Go-heap chunks of chunkSize elements and
// takes them all back at once. It is not safe for concurrent use. Everything
// it hands out stays valid until Reset or Free.
type Arena[T any] struct {
	c heapChunks[T]
}

// NewArena returns an Arena whose chunks hold chunkSize elements. It allocates
// the first chunk immediately.
func NewArena[T any](chunkSize int) (*Arena[T], error) {
	c, err := newHeapChunks[T](chunkSize)
	if err != nil {
		return nil, err
	}
	return &Arena[T]{c: c}, nil
}

// Alloc returns a pointer to a new zeroed T.
func (a *Arena[T]) Alloc() *T { return a.c.take() }

// AllocSlice returns n new zeroed Ts, with capacity n so that appending to it
// never overwrites a neighbour. Requests larger than the chunk size get a
// slice of their own, which Reset zeroes and drops. It panics if n is
// negative.
func (a *Arena[T]) AllocSlice(n int) []T {
	if n < 0 {
		panic(fmt.Sprintf("heapslab: AllocSlice(%d): negative length", n))
	}
	return a.c.takeSlice(n)
}

// Reset zeroes everything a handed out, so that nothing it referenced is kept
// alive by a, and makes the chunks available again. Pointers and slices handed
// out before stay valid memory but now alias new values.
func (a *Arena[T]) Reset() { a.c.reset() }

// Free zeroes everything a handed out and drops the chunks for the garbage
// collector. a must not be used afterwards.
func (a *Arena[T]) Free() { a.c.free() }
