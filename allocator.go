package memory

import (
	"errors"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Allocator is the memory source every function and container in this module
// takes. It is implemented by *Local and *Shared only.
type Allocator interface {
	alloc(size, align int, f fill) (unsafe.Pointer, error)
	realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error)
	free(p unsafe.Pointer, size int) error
	Stats() Stats
	Footprint() Footprint
}

// Stats describes the allocations an allocator currently holds.
type Stats struct {
	// Live is the number of allocations not yet freed.
	Live int
	// Bytes is the usable size of those allocations, including rounding.
	Bytes int
}

// Footprint describes the address space and memory an allocator holds from
// the operating system, whether or not allocations currently use it.
type Footprint struct {
	// Mapped is the address space held, most of which may not be backed by
	// memory.
	Mapped int
	// Committed is an upper bound on the bytes that may be resident: everything
	// handed out so far and not yet returned to the operating system.
	Committed int
	// Retained is the part of Mapped kept for reuse and not currently allocated.
	Retained int
}

func (f Footprint) plus(o Footprint) Footprint {
	return Footprint{Mapped: f.Mapped + o.Mapped, Committed: f.Committed + o.Committed, Retained: f.Retained + o.Retained}
}

func (s Stats) plus(o Stats) Stats {
	return Stats{Live: s.Live + o.Live, Bytes: s.Bytes + o.Bytes}
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

func allocate(h *heap, d *direct, size, align int, f fill) (unsafe.Pointer, error) {
	if size > smallMax {
		return d.make(size, f)
	}
	return h.alloc(size, classForAligned(size, align), f)
}

func deallocate(h *heap, d *direct, p unsafe.Pointer, size int) error {
	if size > smallMax {
		return d.release(p, size)
	}
	s := spanOf(p)
	verifySlot(s, p, size, h.owner)
	eraseSlot(s, p)
	return h.release(s, p)
}

func reallocate(h *heap, d *direct, p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	if oldSize > smallMax && size > smallMax {
		return d.resize(p, oldSize, size)
	}
	if oldSize > smallMax {
		d.check(p, oldSize)
	} else {
		s := spanOf(p)
		verifySlot(s, p, oldSize, h.owner)
		if size <= int(s.slot) && (2*size >= int(s.slot) || classFor(size) == s.class) {
			clearFrom(p, oldSize, size)
			stampSlot(s, p, size)
			return p, nil
		}
	}
	moved, err := allocate(h, d, size, wordAlign, zeroed)
	if err != nil {
		return nil, err
	}
	n := min(oldSize, size)
	copy(unsafe.Slice((*byte)(moved), n), unsafe.Slice((*byte)(p), n))
	return moved, deallocate(h, d, p, oldSize)
}

// Local is an Allocator for a single goroutine. Its zero value is ready for
// use, and it must not be copied after first use. Using it from several
// goroutines at once panics, unless built with memory_unchecked; use Shared
// for that.
type Local struct {
	_ noCopy
	g guard
	h heap
	d direct
}

func (l *Local) alloc(size, align int, f fill) (unsafe.Pointer, error) {
	l.g.enter()
	defer l.g.exit()
	return allocate(&l.h, &l.d, size, align, f)
}

func (l *Local) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	l.g.enter()
	defer l.g.exit()
	return reallocate(&l.h, &l.d, p, oldSize, size)
}

func (l *Local) free(p unsafe.Pointer, size int) error {
	l.g.enter()
	defer l.g.exit()
	return deallocate(&l.h, &l.d, p, size)
}

// Stats reports the allocations l currently holds.
func (l *Local) Stats() Stats {
	l.g.enter()
	defer l.g.exit()
	stats, _ := l.d.snapshot()
	return stats.plus(l.h.stats)
}

// Footprint reports the memory l holds from the operating system.
func (l *Local) Footprint() Footprint {
	l.g.enter()
	defer l.g.exit()
	_, foot := l.d.snapshot()
	return foot.plus(l.h.footprint())
}

// Trim returns retained empty pages to the operating system.
func (l *Local) Trim() error {
	l.g.enter()
	defer l.g.exit()
	return errors.Join(l.d.trimLocked(), l.h.trim())
}

// Close unmaps every page of l, invalidating everything allocated from it, and
// leaves l ready for reuse. Handles from before Close are rejected afterwards.
func (l *Local) Close() error {
	l.g.enter()
	defer l.g.exit()
	return errors.Join(l.d.closeLocked(), l.h.close())
}

type shard struct {
	mu sync.Mutex
	h  heap
}

// Shared is an Allocator safe for concurrent use. Each goroutine works in a
// shard of its own for the length of a call, so calls on different processors
// do not contend, and memory freed on another shard goes back to its owner
// without a lock. Its zero value is ready for use. Containers built on it are
// still not safe for concurrent use.
type Shared struct {
	mu     sync.Mutex
	d      direct
	cache  sync.Pool
	shards atomic.Pointer[[]*shard]
	owner  uint32
}

func (s *Shared) shardAt(i uint32) *shard { return (*s.shards.Load())[i] }

func (s *Shared) all() []*shard {
	if list := s.shards.Load(); list != nil {
		return *list
	}
	return nil
}

func (s *Shared) acquire() *shard {
	if sh, ok := s.cache.Get().(*shard); ok {
		sh.mu.Lock()
		return sh
	}
	for _, sh := range s.all() {
		if sh.mu.TryLock() {
			return sh
		}
	}
	return s.grow()
}

func (s *Shared) grow() *shard {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner == 0 {
		s.owner = nextOwner()
	}
	old := s.all()
	sh := &shard{}
	sh.h.peers, sh.h.owner, sh.h.shard = s, s.owner, uint32(len(old))
	sh.mu.Lock()
	next := make([]*shard, len(old)+1)
	copy(next, old)
	next[len(old)] = sh
	s.shards.Store(&next)
	return sh
}

func (s *Shared) put(sh *shard) {
	sh.mu.Unlock()
	s.cache.Put(sh)
}

func (s *Shared) alloc(size, align int, f fill) (unsafe.Pointer, error) {
	if size > smallMax {
		return s.d.make(size, f)
	}
	sh := s.acquire()
	defer s.put(sh)
	return sh.h.alloc(size, classForAligned(size, align), f)
}

func (s *Shared) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	sh := s.acquire()
	defer s.put(sh)
	return reallocate(&sh.h, &s.d, p, oldSize, size)
}

func (s *Shared) free(p unsafe.Pointer, size int) error {
	if size > smallMax {
		return s.d.release(p, size)
	}
	sh := s.acquire()
	defer s.put(sh)
	return deallocate(&sh.h, &s.d, p, size)
}

func (s *Shared) lockAll() []*shard {
	s.mu.Lock()
	shards := s.all()
	for _, sh := range shards {
		sh.mu.Lock()
	}
	return shards
}

func (s *Shared) unlockAll(shards []*shard) {
	for _, sh := range shards {
		sh.mu.Unlock()
	}
	s.mu.Unlock()
}

// Stats reports the allocations s currently holds.
func (s *Shared) Stats() Stats {
	shards := s.lockAll()
	defer s.unlockAll(shards)
	stats, _ := s.d.snapshot()
	for _, sh := range shards {
		_ = sh.h.drain()
		stats = stats.plus(sh.h.stats)
	}
	return stats
}

// Footprint reports the memory s holds from the operating system.
func (s *Shared) Footprint() Footprint {
	shards := s.lockAll()
	defer s.unlockAll(shards)
	_, foot := s.d.snapshot()
	for _, sh := range shards {
		foot = foot.plus(sh.h.footprint())
	}
	return foot
}

// Trim returns retained empty pages to the operating system.
func (s *Shared) Trim() error {
	shards := s.lockAll()
	defer s.unlockAll(shards)
	errs := []error{s.d.trimLocked()}
	for _, sh := range shards {
		errs = append(errs, sh.h.trim())
	}
	return errors.Join(errs...)
}

// Close unmaps every page of s, invalidating everything allocated from it, and
// leaves s ready for reuse.
func (s *Shared) Close() error {
	shards := s.lockAll()
	defer s.unlockAll(shards)
	errs := []error{s.d.closeLocked()}
	s.owner = nextOwner()
	for _, sh := range shards {
		errs = append(errs, sh.h.close())
		sh.h.owner = s.owner
	}
	return errors.Join(errs...)
}
