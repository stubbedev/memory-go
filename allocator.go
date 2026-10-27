package memory

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
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

const (
	headerSize = int(unsafe.Sizeof(header{}))
	liveMagic  = 0xa110ca7ed0ff4ea9
	freedMagic = 0xf7eedf7eedf7eed5
)

type header struct {
	owner uint64
	magic uint64
}

func headerOf(p unsafe.Pointer) *header { return (*header)(unsafe.Add(p, -headerSize)) }

func checkLive(p unsafe.Pointer) {
	if headerOf(p).magic != liveMagic {
		panic(fmt.Sprintf("memory: use of %#x after it was freed", uintptr(p)))
	}
}

var owners atomic.Uint64

// Local is an Allocator for a single goroutine. Its zero value is ready for
// use, and it must not be copied after first use. Using it from several
// goroutines at once panics; use Shared for that.
type Local struct {
	busy  atomic.Bool
	id    uint64
	a     modernc.Allocator
	stats Stats
}

func (l *Local) enter() {
	if !l.busy.CompareAndSwap(false, true) {
		panic("memory: Local used by several goroutines at once; use Shared")
	}
	if l.id == 0 {
		l.id = owners.Add(1)
	}
}

func (l *Local) exit() { l.busy.Store(false) }

func (l *Local) admit(base unsafe.Pointer) unsafe.Pointer {
	*(*header)(base) = header{owner: l.id, magic: liveMagic}
	l.stats.Live++
	l.stats.Bytes += modernc.UnsafeUsableSize(base) - headerSize
	return unsafe.Add(base, headerSize)
}

func (l *Local) retire(p unsafe.Pointer) unsafe.Pointer {
	h := headerOf(p)
	switch {
	case h.magic == freedMagic:
		panic(fmt.Sprintf("memory: %#x is already freed", uintptr(p)))
	case h.magic != liveMagic:
		panic(fmt.Sprintf("memory: %#x is not a live allocation: already freed, or never allocated here", uintptr(p)))
	case h.owner != l.id:
		panic(fmt.Sprintf("memory: %#x belongs to a different allocator", uintptr(p)))
	}
	h.magic = freedMagic
	base := unsafe.Pointer(h)
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
	l.enter()
	defer l.exit()
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
	l.enter()
	defer l.exit()
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
	l.enter()
	defer l.exit()
	return l.a.UnsafeFree(l.retire(p))
}

// Stats reports the allocations l currently holds.
func (l *Local) Stats() Stats {
	l.enter()
	defer l.exit()
	return l.stats
}

// Trim returns retained empty pages to the operating system.
func (l *Local) Trim() error {
	l.enter()
	defer l.exit()
	return l.a.Trim()
}

// Close unmaps every page of l, invalidating everything allocated from it, and
// leaves l ready for reuse. Handles from before Close are rejected afterwards.
func (l *Local) Close() error {
	l.enter()
	defer l.exit()
	l.id, l.stats = 0, Stats{}
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
