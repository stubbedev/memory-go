// Package heapref lets off-heap memory refer to values on the Go heap.
//
// Off-heap memory may not hold Go pointers, because the garbage collector cannot
// see them there. A Table keeps values of any type, such as funcs, interfaces,
// maps or closures, in an ordinary Go slice the collector does scan, and names
// each by a pointer-free Ref that may be stored off-heap. Weak does the same
// without keeping its targets alive, and Funcs names functions registered once
// by a 4-byte FuncID.
//
// A Ref carries the generation of its slot, so with the guards compiled in, using
// a Ref after Delete, or one from another Table, panics. Building with the
// memory_unchecked tag compiles these checks out, leaving only Go's bounds checks.
//
// None of the types here is safe for concurrent use.
package heapref

import (
	"fmt"
	"math"
	"sync/atomic"

	memory "github.com/stubbedev/memory-go"
)

const idSpacing = 0x9e3779b9

var ids atomic.Uint32

// Ref names a value held by a Table in eight bytes and may be stored off-heap.
// The zero Ref is nil.
type Ref[T any] struct {
	slot uint32
	gen  uint32
}

// IsNil reports whether r refers to nothing.
func (r Ref[T]) IsNil() bool { return r.slot == 0 }

type slot[T any] struct {
	value T
	gen   uint32
	next  uint32
}

// Table holds values on the Go heap until they are deleted. The zero Table is
// empty and ready to use; it must not be copied after first use. It is not safe
// for concurrent use.
type Table[T any] struct {
	slots []slot[T]
	free  uint32
	id    uint32
	n     int
}

// Len returns the number of values held.
func (t *Table[T]) Len() int { return t.n }

// Put stores v and returns its Ref. It fails only once 2³²−1 slots are in use.
func (t *Table[T]) Put(v T) (Ref[T], error) {
	if t.free != 0 {
		i := t.free
		s := &t.slots[i-1]
		t.free = s.next
		s.value, s.gen, s.next = v, s.gen+1, 0
		t.n++
		return Ref[T]{slot: i, gen: s.gen}, nil
	}
	if int64(len(t.slots)) >= math.MaxUint32 {
		return Ref[T]{}, fmt.Errorf("heapref: %d values: %w", len(t.slots), memory.ErrTooLarge)
	}
	if t.id == 0 {
		t.id = ids.Add(1)*idSpacing | 1
	}
	t.slots = append(t.slots, slot[T]{value: v, gen: t.id})
	t.n++
	return Ref[T]{slot: uint32(len(t.slots)), gen: t.id}, nil //nolint:gosec // bounded above
}

func (t *Table[T]) at(r Ref[T], op string) *slot[T] {
	checkRef(len(t.slots), r.slot, op)
	s := &t.slots[r.slot-1]
	checkGen(s.gen, r.gen, op)
	return s
}

// Get returns the value r names. It panics if r is nil, and with the guards
// compiled in, if r was deleted or comes from another Table.
func (t *Table[T]) Get(r Ref[T]) T { return t.at(r, "Get").value }

// Set replaces the value r names, panicking as Get does.
func (t *Table[T]) Set(r Ref[T], v T) { t.at(r, "Set").value = v }

// Delete removes the value r names, so the collector may reclaim it once nothing
// else refers to it, and invalidates r. It panics as Get does, including when r
// was already deleted.
func (t *Table[T]) Delete(r Ref[T]) {
	s := t.at(r, "Delete")
	var zero T
	s.value, s.gen, s.next = zero, s.gen+1, t.free
	t.free = r.slot
	t.n--
}
