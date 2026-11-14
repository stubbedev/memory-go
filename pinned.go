package memory

import (
	"fmt"
	"runtime"
	"sync"
)

// Pinned is a Go pointer to a T on the Go heap that may be stored off-heap,
// the way cgo lets C memory hold pointers to pinned Go objects. Pin creates
// it through a Pins, which keeps the T alive and unmoved until Unpin; the
// garbage collector never sees the off-heap copy. The zero Pinned is nil.
//
// Only pointers can be pinned. To keep a func or another pointer-shaped value
// off-heap, pin a pointer to a variable holding it, such as a *func().
type Pinned[T any] struct{ p *T }

func (Pinned[T]) offHeap() {}

// Value returns the pinned pointer. It is valid until the T is unpinned;
// using it after Unpin is not detected.
func (p Pinned[T]) Value() *T { return p.p }

// IsNil reports whether p refers to nothing.
func (p Pinned[T]) IsNil() bool { return p.p == nil }

// Pins pins Go objects for Pinned handles and holds them on the Go heap
// until they are unpinned, so it must stay reachable from Go for as long as
// any of its pins are live: dropping a Pins with live pins makes the runtime
// panic about a leaking pinned pointer. Its zero value is ready for use, it
// is safe for concurrent use, and it must not be copied after first use.
type Pins struct {
	mu   sync.Mutex
	live map[any]*pin
}

type pin struct {
	pinner runtime.Pinner
	n      int
}

// Pin pins the object p points to in ps and returns a handle to it. Pinning
// the same pointer again counts, and each Pin needs its own Unpin. A nil p
// returns the nil Pinned. The runtime refuses pointers into a Go arena.
func Pin[T any](ps *Pins, p *T) Pinned[T] {
	if p == nil {
		return Pinned[T]{}
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	e := ps.live[p]
	if e == nil {
		e = &pin{}
		e.pinner.Pin(p)
		if ps.live == nil {
			ps.live = map[any]*pin{}
		}
		ps.live[p] = e
	}
	e.n++
	return Pinned[T]{p: p}
}

// Unpin clears *p and then releases the pin it held, so the handle being
// unpinned never holds a pointer the garbage collector may since have freed.
// Clear every other copy of the handle first, for the same reason. Unpinning
// the nil Pinned does nothing; unpinning a pointer ps does not hold pinned
// panics.
func Unpin[T any](ps *Pins, p *Pinned[T]) {
	ptr := p.p
	if ptr == nil {
		return
	}
	*p = Pinned[T]{}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	e := ps.live[ptr]
	if e == nil {
		panic(fmt.Sprintf("memory: unpin of %p, which is not pinned here", ptr))
	}
	if e.n--; e.n == 0 {
		e.pinner.Unpin()
		delete(ps.live, ptr)
	}
}

// Len returns the number of distinct objects ps holds pinned.
func (ps *Pins) Len() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.live)
}

// UnpinAll releases every pin held by ps and leaves it ready for reuse. Every
// Pinned handle from ps must be cleared first or never used again.
func (ps *Pins) UnpinAll() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, e := range ps.live {
		e.pinner.Unpin()
	}
	clear(ps.live)
}
