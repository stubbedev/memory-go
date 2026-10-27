package memory

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"

	modernc "modernc.org/memory"
)

// Allocator is the memory source every function and container in this module
// takes. It is implemented by *Local and *Shared only.
type Allocator interface {
	calloc(size int) (unsafe.Pointer, error)
	realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error)
	free(p unsafe.Pointer, size int) error
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
	d     direct
	stats Stats
}

func withHeader(size int) (int, error) {
	if size > math.MaxInt-headerSize {
		return 0, ErrTooLarge
	}
	return size + headerSize, nil
}

func baseOf(p unsafe.Pointer) unsafe.Pointer { return unsafe.Add(p, -headerSize) }

func (l *Local) acquire(total int) (unsafe.Pointer, int, error) {
	if isDirect(total) {
		return l.d.alloc(total)
	}
	base, err := l.a.UnsafeCalloc(total)
	if err != nil {
		return nil, 0, err
	}
	return base, modernc.UnsafeUsableSize(base), nil
}

func (l *Local) release(base unsafe.Pointer, total int) error {
	if isDirect(total) {
		return l.d.free(base)
	}
	return l.a.UnsafeFree(base)
}

func (l *Local) admit(base unsafe.Pointer, reserved, size int) unsafe.Pointer {
	l.stats.Live++
	l.stats.Bytes += reserved - headerSize
	return l.g.stamp(base, size)
}

func (l *Local) retire(p unsafe.Pointer, size, total int) (unsafe.Pointer, int) {
	base := baseOf(p)
	var reserved int
	if isDirect(total) {
		var ok bool
		if reserved, ok = l.d.reserved(base); !ok {
			panic(fmt.Sprintf("memory: %#x is not a live allocation of this allocator: already freed, from another allocator, or never allocated", uintptr(p)))
		}
		l.g.verify(p, size)
	} else {
		l.g.verify(p, size)
		reserved = modernc.UnsafeUsableSize(base)
	}
	l.stats.Live--
	l.stats.Bytes -= reserved - headerSize
	return base, reserved
}

func (l *Local) calloc(size int) (unsafe.Pointer, error) {
	l.g.enter()
	defer l.g.exit()
	total, err := withHeader(size)
	if err != nil {
		return nil, err
	}
	base, reserved, err := l.acquire(total)
	if err != nil {
		return nil, err
	}
	return l.admit(base, reserved, size), nil
}

func (l *Local) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	l.g.enter()
	defer l.g.exit()
	oldTotal, err := withHeader(oldSize)
	if err != nil {
		return nil, err
	}
	total, err := withHeader(size)
	if err != nil {
		return nil, err
	}
	base, reserved := l.retire(p, oldSize, oldTotal)
	if !isDirect(oldTotal) && !isDirect(total) {
		moved, err := l.a.UnsafeRealloc(base, total)
		if err != nil {
			l.admit(base, reserved, oldSize)
			return nil, err
		}
		return l.admit(moved, modernc.UnsafeUsableSize(moved), size), nil
	}
	if isDirect(oldTotal) && isDirect(total) {
		if rounded, err := pageRound(total); err == nil && rounded == reserved {
			return l.admit(base, reserved, size), nil
		}
	}
	moved, movedReserved, err := l.acquire(total)
	if err != nil {
		l.admit(base, reserved, oldSize)
		return nil, err
	}
	n := min(oldSize, size)
	copy(unsafe.Slice((*byte)(unsafe.Add(moved, headerSize)), n), unsafe.Slice((*byte)(p), n))
	q := l.admit(moved, movedReserved, size)
	return q, l.release(base, oldTotal)
}

func (l *Local) free(p unsafe.Pointer, size int) error {
	l.g.enter()
	defer l.g.exit()
	total, err := withHeader(size)
	if err != nil {
		return err
	}
	base, _ := l.retire(p, size, total)
	return l.release(base, total)
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
	return errors.Join(l.d.trim(), l.a.Trim())
}

// Close unmaps every page of l, invalidating everything allocated from it, and
// leaves l ready for reuse. Handles from before Close are rejected afterwards.
func (l *Local) Close() error {
	l.g.enter()
	defer l.g.exit()
	l.g.reset()
	l.stats = Stats{}
	return errors.Join(l.d.close(), l.a.Close())
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

func (s *Shared) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.realloc(p, oldSize, size)
}

func (s *Shared) free(p unsafe.Pointer, size int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.free(p, size)
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
