package memory

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	guardedClass = 0xff
	guardedTable = (int(sideOffset) + 7) &^ 7
	guardedFreed = -1
	guardedMoved = -2
)

var guardedLarge struct {
	n      atomic.Int64
	chunks sync.Map
}

func guardedEntry(base, p unsafe.Pointer) *atomic.Int64 {
	page := (uintptr(p) - uintptr(base)) / uintptr(osPageSize)
	return (*atomic.Int64)(unsafe.Add(base, guardedTable+8*int(page)))
}

type guardedChunk struct {
	base        unsafe.Pointer
	size        int
	next        int
	live        int
	quarantined int
	large       bool
	dead        []unsafe.Pointer
}

type guardedAlloc struct {
	chunk *guardedChunk
	start unsafe.Pointer
	pages int
	size  int
	align int
}

// Guarded is a debugging Allocator that turns use after free and buffer overruns
// into faults at the point of the mistake. Every allocation gets pages of its own,
// with the value placed flush against a following inaccessible guard page, and a
// freed allocation's pages are made inaccessible and their addresses never handed
// out again. So reading or writing freed memory faults through any route: a Ptr,
// Slice or String, a Ref or View, or a Go pointer or slice obtained from a
// handle; so does running past the end of an allocation. Reading a freed Ptr,
// Slice or String through its handle panics with a message instead. Running
// before the start is caught only past the start of the value's first page, and
// when a larger alignment is requested the padding after the value up to the next
// multiple of it is not guarded either.
//
// Realloc always moves, quarantining the old pages. Freeing twice, freeing or
// resizing a stale or foreign handle, or with the wrong size panics.
//
// Each allocation costs at least two pages of address space and one of memory,
// and on Linux one memory mapping of the vm.max_map_count limit, which is 65530 by
// default, so it suits tests rather than production. Freed address space stays
// reserved until Close, unless NewGuarded set a quarantine budget. Guarded is safe
// for concurrent use; its zero value is ready for use and keeps every freed range
// until Close.
type Guarded struct {
	mu          sync.Mutex
	owner       uint32
	limit       int
	limited     bool
	live        map[unsafe.Pointer]guardedAlloc
	dead        map[unsafe.Pointer]bool
	chunks      map[unsafe.Pointer]*guardedChunk
	cur         *guardedChunk
	retired     []*guardedChunk
	stats       Stats
	mapped      int
	committed   int
	quarantined int
}

// NewGuarded returns a Guarded that keeps at most quarantine bytes of freed address
// space reserved. Beyond that it returns the oldest blocks whose allocations are all
// freed to the operating system, oldest first. Their addresses still fault until the
// operating system maps something else there, which can then hide a use after free.
// A block holding a live allocation is never returned, so the quarantine can exceed
// the budget. A negative quarantine keeps everything until Close.
func NewGuarded(quarantine int) *Guarded {
	return &Guarded{limit: quarantine, limited: quarantine >= 0}
}

func roundUp(n, align int) (int, error) {
	if n > math.MaxInt-align+1 {
		return 0, ErrTooLarge
	}
	return (n + align - 1) &^ (align - 1), nil
}

func (g *Guarded) alloc(size, align int, _ fill) (unsafe.Pointer, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.allocLocked(size, align)
}

func (g *Guarded) allocLocked(size, align int) (unsafe.Pointer, error) {
	body, err := roundUp(size, align)
	if err != nil {
		return nil, err
	}
	pages, err := pageRound(body)
	if err != nil {
		return nil, err
	}
	c, at, err := g.place(size, pages)
	if err != nil {
		return nil, err
	}
	start := unsafe.Add(c.base, at)
	if err := guardedCommit(start, pages); err != nil {
		return nil, fmt.Errorf("commit %d bytes at %#x: %w", pages, uintptr(start), err)
	}
	if c.large {
		c.next = c.size
	} else {
		c.next = at + pages + osPageSize
	}
	p := unsafe.Add(start, pages-body)
	guardedEntry(c.base, p).Store(int64(size))
	if g.live == nil {
		g.live = map[unsafe.Pointer]guardedAlloc{}
	}
	g.live[p] = guardedAlloc{chunk: c, start: start, pages: pages, size: size, align: align}
	c.live++
	g.stats.Live++
	g.stats.Bytes += size
	g.committed += pages
	return p, nil
}

func (g *Guarded) place(size, pages int) (*guardedChunk, int, error) {
	at := 2 * osPageSize
	if size > smallMax {
		if pages > math.MaxInt/2-spanSize {
			return nil, 0, ErrTooLarge
		}
		c, err := g.mapChunk(at+pages+osPageSize, true)
		return c, at, err
	}
	if c := g.cur; c != nil && c.next+pages+osPageSize <= c.size {
		return c, c.next, nil
	}
	c, err := g.mapChunk(spanSize, false)
	if err != nil {
		return nil, 0, err
	}
	if old := g.cur; old != nil && old.live == 0 {
		g.retired = append(g.retired, old)
	}
	g.cur = c
	return c, at, nil
}

func (g *Guarded) mapChunk(size int, large bool) (*guardedChunk, error) {
	base, err := guardedReserve(size, spanSize)
	if err != nil {
		return nil, fmt.Errorf("reserve %d bytes: %w", size, err)
	}
	if err := guardedCommit(base, osPageSize); err != nil {
		return nil, errors.Join(fmt.Errorf("commit %d bytes at %#x: %w", osPageSize, uintptr(base), err), unmap(base, size))
	}
	if g.owner == 0 {
		g.owner = nextOwner()
	}
	*(*span)(base) = span{slot: 1, owner: g.owner, class: guardedClass}
	c := &guardedChunk{base: base, size: size, large: large}
	if g.chunks == nil {
		g.chunks = map[unsafe.Pointer]*guardedChunk{}
	}
	g.chunks[base] = c
	g.mapped += size
	g.committed += osPageSize
	if large && Checked {
		guardedLarge.chunks.Store(base, size)
		guardedLarge.n.Add(1)
	}
	return c, nil
}

func (g *Guarded) verify(p unsafe.Pointer, size int) guardedAlloc {
	a, ok := g.live[p]
	if !ok {
		moved, dead := g.dead[p]
		switch {
		case dead && moved:
			panic(fmt.Sprintf("memory: %#x was resized since this handle was made, which already freed that handle", uintptr(p)))
		case dead:
			panic(fmt.Sprintf("memory: %#x is already freed", uintptr(p)))
		}
		panic(fmt.Sprintf("memory: %#x is not a live allocation of this Guarded allocator: from another allocator, freed and since unmapped, or never allocated", uintptr(p)))
	}
	if a.size != size {
		panic(fmt.Sprintf("memory: %#x was allocated as %d bytes, not %d", uintptr(p), a.size, size))
	}
	return a
}

func (g *Guarded) free(p unsafe.Pointer, size int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.release(p, g.verify(p, size), false)
}

func (g *Guarded) release(p unsafe.Pointer, a guardedAlloc, moved bool) error {
	c := a.chunk
	mark := int64(guardedFreed)
	if moved {
		mark = guardedMoved
	}
	guardedEntry(c.base, p).Store(mark)
	delete(g.live, p)
	if g.dead == nil {
		g.dead = map[unsafe.Pointer]bool{}
	}
	g.dead[p] = moved
	c.dead = append(c.dead, p)
	var err error
	if rerr := guardedRevoke(a.start, a.pages); rerr != nil {
		err = fmt.Errorf("revoke %d bytes at %#x: %w", a.pages, uintptr(a.start), rerr)
	}
	g.stats.Live--
	g.stats.Bytes -= a.size
	g.committed -= a.pages
	g.quarantined += a.pages + osPageSize
	c.quarantined += a.pages + osPageSize
	c.live--
	if c.live == 0 && c != g.cur {
		g.retired = append(g.retired, c)
	}
	return errors.Join(err, g.evict())
}

func (g *Guarded) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.verify(p, oldSize)
	q, err := g.allocLocked(size, a.align)
	if err != nil {
		return nil, err
	}
	n := min(oldSize, size)
	copy(unsafe.Slice((*byte)(q), n), unsafe.Slice((*byte)(p), n))
	return q, g.release(p, a, true)
}

func (g *Guarded) evict() error {
	var errs []error
	for g.limited && g.quarantined > g.limit && len(g.retired) > 0 {
		c := g.retired[0]
		g.retired = g.retired[1:]
		errs = append(errs, g.unmapChunk(c))
	}
	return errors.Join(errs...)
}

func (g *Guarded) unmapChunk(c *guardedChunk) error {
	if c.large && Checked {
		guardedLarge.chunks.Delete(c.base)
		guardedLarge.n.Add(-1)
	}
	for _, p := range c.dead {
		delete(g.dead, p)
	}
	delete(g.chunks, c.base)
	g.mapped -= c.size
	g.committed -= osPageSize
	g.quarantined -= c.quarantined
	return unmap(c.base, c.size)
}

// Stats reports the allocations g currently holds. Bytes counts the sizes
// requested, since nothing is rounded up.
func (g *Guarded) Stats() Stats {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stats
}

// Footprint reports the memory g holds from the operating system. Mapped
// includes guard pages and quarantined address space, Committed counts the pages
// of live allocations and one bookkeeping page per block, and Retained is the
// quarantined address space of freed allocations.
func (g *Guarded) Footprint() Footprint {
	g.mu.Lock()
	defer g.mu.Unlock()
	return Footprint{Mapped: g.mapped, Committed: g.committed, Retained: g.quarantined}
}

// Close unmaps every page of g, invalidating everything allocated from it, and
// leaves g ready for reuse with the same quarantine budget. Using a handle from
// before Close faults, unless the operating system has mapped its address again.
func (g *Guarded) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	errs := make([]error, 0, len(g.chunks))
	for _, c := range g.chunks {
		errs = append(errs, g.unmapChunk(c))
	}
	g.live, g.dead, g.chunks, g.cur, g.retired = nil, nil, nil, nil, nil
	g.stats, g.mapped, g.committed, g.quarantined = Stats{}, 0, 0, 0
	return errors.Join(errs...)
}
