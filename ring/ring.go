// Package ring provides a bounded lock-free queue from one goroutine to another,
// whose elements live off the Go heap. It moves pointer-free values between
// goroutines without allocating, and each side reads the other's position only when
// its cached copy runs out, which keeps the two cores off each other's cache lines.
package ring

import (
	"fmt"
	"math/bits"
	"sync/atomic"

	memory "github.com/stubbedev/memory-go"
)

const cacheLine = 64

// SPSC is a queue with exactly one producer goroutine, calling Push, and exactly
// one consumer goroutine, calling Pop. More of either corrupts it.
type SPSC[T any] struct {
	alloc      memory.Allocator
	store      memory.Slice[T]
	elems      []T
	mask       uint64
	_          [cacheLine]byte
	tail       atomic.Uint64
	cachedHead uint64
	_          [cacheLine]byte
	head       atomic.Uint64
	cachedTail uint64
	_          [cacheLine]byte
}

// New returns an empty queue holding at least capacity elements, rounded up to a
// power of two.
func New[T any](a memory.Allocator, capacity int) (*SPSC[T], error) {
	if capacity < 0 {
		return nil, fmt.Errorf("ring: capacity %d: %w", capacity, memory.ErrNegativeLen)
	}
	size := 1
	if capacity > 1 {
		if capacity > 1<<(bits.UintSize-2) {
			return nil, fmt.Errorf("ring: capacity %d: %w", capacity, memory.ErrTooLarge)
		}
		size = 1 << bits.Len(uint(capacity-1))
	}
	store, err := memory.MakeSlice[T](a, size)
	if err != nil {
		return nil, err
	}
	return &SPSC[T]{alloc: a, store: store, elems: store.Elems(), mask: uint64(size) - 1}, nil
}

// Cap returns the number of elements the queue holds when full.
func (q *SPSC[T]) Cap() int { return len(q.elems) }

// Push adds value, or reports false if the queue is full. Only the producer may
// call it.
func (q *SPSC[T]) Push(value T) bool {
	tail := q.tail.Load()
	if tail-q.cachedHead > q.mask {
		q.cachedHead = q.head.Load()
		if tail-q.cachedHead > q.mask {
			return false
		}
	}
	q.elems[tail&q.mask] = value
	q.tail.Store(tail + 1)
	return true
}

// Pop removes the oldest value, or reports false if the queue is empty. Only the
// consumer may call it.
func (q *SPSC[T]) Pop() (T, bool) {
	head := q.head.Load()
	if head == q.cachedTail {
		q.cachedTail = q.tail.Load()
		if head == q.cachedTail {
			var zero T
			return zero, false
		}
	}
	value := q.elems[head&q.mask]
	q.head.Store(head + 1)
	return value, true
}

// Free releases the elements. No goroutine may be using q.
func (q *SPSC[T]) Free() error {
	q.elems = nil
	return memory.FreeSlice(q.alloc, q.store)
}
