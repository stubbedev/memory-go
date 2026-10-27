// Package memory allocates typed values outside the Go heap without cgo.
//
// Memory comes from modernc.org/memory, a pure Go allocator that maps pages
// from the operating system directly. The garbage collector neither scans nor
// frees it, so every value placed there must be free of Go pointers: no
// pointers, slices, strings, maps, channels, functions or interfaces. Every
// allocation checks this and returns ErrHoldsPointers otherwise.
//
// Local serves a single goroutine; Shared serialises access for several.
package memory

import (
	"sync"
	"unsafe"

	modernc "modernc.org/memory"
)

type Allocator interface {
	calloc(size int) (unsafe.Pointer, error)
	realloc(p unsafe.Pointer, size int) (unsafe.Pointer, error)
	free(p unsafe.Pointer) error
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

type Local struct {
	_ noCopy
	a modernc.Allocator
}

func (l *Local) calloc(size int) (unsafe.Pointer, error) { return l.a.UnsafeCalloc(size) }

func (l *Local) realloc(p unsafe.Pointer, size int) (unsafe.Pointer, error) {
	return l.a.UnsafeRealloc(p, size)
}

func (l *Local) free(p unsafe.Pointer) error { return l.a.UnsafeFree(p) }

func (l *Local) Trim() error { return l.a.Trim() }

func (l *Local) Close() error { return l.a.Close() }

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

func (s *Shared) Trim() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.Trim()
}

func (s *Shared) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.l.Close()
}
