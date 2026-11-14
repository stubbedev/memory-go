package heapref

import "weak"

// WeakRef names a pointer held by a Weak in eight bytes and may be stored
// off-heap. The zero WeakRef is nil.
type WeakRef[T any] struct {
	r Ref[weak.Pointer[T]]
}

// IsNil reports whether r refers to nothing.
func (r WeakRef[T]) IsNil() bool { return r.r.IsNil() }

// Weak holds pointers without keeping their targets alive, so an off-heap cache
// can refer to Go objects the program may still drop. A slot outlives its
// target: Get then returns nil, and the slot stays in use until Delete. The zero
// Weak is empty and ready to use; it must not be copied after first use. It is
// not safe for concurrent use.
type Weak[T any] struct {
	t Table[weak.Pointer[T]]
}

// Len returns the number of slots in use, including those whose target has
// been collected.
func (w *Weak[T]) Len() int { return w.t.Len() }

// Put stores a weak pointer to p and returns its WeakRef. It fails as Table.Put
// does.
func (w *Weak[T]) Put(p *T) (WeakRef[T], error) {
	r, err := w.t.Put(weak.Make(p))
	return WeakRef[T]{r: r}, err
}

// Get returns the pointer r names, or nil once its target has been collected.
// It panics as Table.Get does.
func (w *Weak[T]) Get(r WeakRef[T]) *T { return w.t.Get(r.r).Value() }

// Set makes r name p instead, panicking as Table.Get does.
func (w *Weak[T]) Set(r WeakRef[T], p *T) { w.t.Set(r.r, weak.Make(p)) }

// Delete frees the slot r names and invalidates r, panicking as Table.Delete
// does.
func (w *Weak[T]) Delete(r WeakRef[T]) { w.t.Delete(r.r) }
