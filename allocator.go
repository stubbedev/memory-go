package memory

import (
	"math"
	"sync"
	"unsafe"

	modernc "modernc.org/memory"
)

// Allocator is the memory source every function and container in this module
// takes. It is implemented by *Local and *Shared only.
type Allocator interface {
	calloc(size int) (unsafe.Pointer, error)
	realloc(p unsafe.Pointer, size int) (unsafe.Pointer, error)
	free(p unsafe.Pointer) error
}

// Stats describes the allocations an allocator currently holds.
type Stats struct {
	// Live is the number of allocations not yet freed.
	Live int
	// Bytes is the usable size of those allocations, including rounding.
	Bytes int
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Local is an Allocator for a single goroutine. Its zero value is ready for
// use, and it must not be copied after first use. Using it from several
// goroutines at once panics, unless built with memory_unchecked; use Shared
// for that.
type Local struct {
	_     noCopy
	g     guard
	a     modernc.Allocator
	stats Stats
}

func (l *Local) admit(base unsafe.Pointer) unsafe.Pointer {
	l.stats.Live++
	l.stats.Bytes += modernc.UnsafeUsableSize(base) - headerSize
	return l.g.stamp(base)
}

func (l *Local) retire(p unsafe.Pointer) unsafe.Pointer {
	base := l.g.verify(p)
	l.stats.Live--
	l.stats.Bytes -= modernc.UnsafeUsableSize(base) - headerSize
	return base
}

func withHeader(size int) (int, error) {
	if size > math.MaxInt-headerSize {
		return 0, ErrTooLarge
	}
	return size + headerSize, nil
}

func (l *Local) calloc(size int) (unsafe.Pointer, error) {
	l.g.enter()
	defer l.g.exit()
	total, err := withHeader(size)
	if err != nil {
		return nil, err
	}
	base, err := l.a.UnsafeCalloc(total)
	if err != nil {
		return nil, err
	}
	return l.admit(base), nil
}

func (l *Local) realloc(p unsafe.Pointer, size int) (unsafe.Pointer, error) {
	l.g.enter()
	defer l.g.exit()
	total, err := withHeader(size)
	if err != nil {
		return nil, err
	}
	base := l.retire(p)
	moved, err := l.a.UnsafeRealloc(base, total)
	if err != nil {
		l.admit(base)
		return nil, err
	}
	return l.admit(moved), nil
}

func (l *Local) free(p unsafe.Pointer) error {
	l.g.enter()
	defer l.g.exit()
	return l.a.UnsafeFree(l.retire(p))
}

// Stats reports the allocations l currently holds.
func (l *Local) Stats() Stats {
	l.g.enter()
	defer l.g.exit()
	return l.stats
}

// Trim returns retained empty pages to the operating system.
func (l *Local) Trim() error {
	l.g.enter()
	defer l.g.exit()
	return l.a.Trim()
}

// Close unmaps every page of l, invalidating everything allocated from it, and
// leaves l ready for reuse. Handles from before Close are rejected afterwards.
func (l *Local) Close() error {
	l.g.enter()
	defer l.g.exit()
	l.g.reset()
	l.stats = Stats{}
	return l.a.Close()
}

// Shared is an Allocator safe for concurrent use: a mutex serialises every
// call. Its zero value is ready for use. Containers built on it are still not
// safe for concurrent use.
type Shared struct {
	mu sync.Mutex
	l  Local
}

func (s *Shared) calloc(size int) (unsafe.Pointer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.calloc(size)
}

func (s *Shared) realloc(p unsafe.Pointer, size int) (unsafe.Pointer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.realloc(p, size)
}

func (s *Shared) free(p unsafe.Pointer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.free(p)
}

// Stats reports the allocations s currently holds.
func (s *Shared) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.Stats()
}

// Trim returns retained empty pages to the operating system.
func (s *Shared) Trim() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.Trim()
}

// Close unmaps every page of s, invalidating everything allocated from it, and
// leaves s ready for reuse.
func (s *Shared) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.Close()
}
