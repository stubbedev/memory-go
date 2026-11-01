// Package bufpool recycles off-heap byte buffers of power-of-two sizes, for I/O
// and decoding that would otherwise churn []byte through the Go heap.
//
// Sizes start at 4 KiB, and every buffer is aligned to its size up to the
// operating system's page, which direct I/O and registered ring buffers need.
// Contents of a buffer are undefined when handed out, because the point is not
// paying to clear what a read is about to overwrite.
package bufpool

import (
	"errors"
	"fmt"
	"math/bits"
	"slices"
	"sync"

	memory "github.com/stubbedev/memory-go"
)

const (
	minLog   = 12
	numClass = 20
)

// ErrForeign reports a Buf that did not come from this pool or was put back
// already.
var ErrForeign = errors.New("buffer is not checked out of this pool")

// Buf is a buffer checked out of a Pool. It may be stored off-heap.
type Buf struct {
	s memory.Slice[byte]
	n int
}

// Bytes returns the buffer with the length asked for and room up to Cap, valid
// until Put.
func (b Buf) Bytes() []byte { return b.s.Elems()[:b.n] }

// Cap returns the size of the buffer.
func (b Buf) Cap() int { return b.s.Len() }

// Pool hands out buffers and keeps returned ones for reuse, up to a limit. It is
// safe for concurrent use if its allocator is a Shared.
type Pool struct {
	alloc    memory.Allocator
	mu       sync.Mutex
	free     [numClass][]memory.Slice[byte]
	retained int
	limit    int
}

// New returns a Pool keeping at most maxRetained bytes of returned buffers.
func New(a memory.Allocator, maxRetained int) (*Pool, error) {
	if maxRetained < 0 {
		return nil, fmt.Errorf("bufpool: retain %d bytes: %w", maxRetained, memory.ErrNegativeLen)
	}
	return &Pool{alloc: a, limit: maxRetained}, nil
}

func classOf(n int) int {
	if n <= 1<<minLog {
		return 0
	}
	return bits.Len(uint(n-1)) - minLog
}

// Get returns a buffer of length n with undefined contents. Requests too large
// to pool are allocated and freed directly.
func (p *Pool) Get(n int) (Buf, error) {
	if n < 0 {
		return Buf{}, fmt.Errorf("bufpool: get %d bytes: %w", n, memory.ErrNegativeLen)
	}
	c := classOf(n)
	if c >= numClass {
		s, err := memory.MakeSliceUninit[byte](p.alloc, n)
		return Buf{s: s, n: n}, err
	}
	if s, ok := p.pop(c); ok {
		return Buf{s: s, n: n}, nil
	}
	s, err := memory.MakeSliceAlignedUninit[byte](p.alloc, 1<<(minLog+c), 1<<minLog)
	return Buf{s: s, n: n}, err
}

func (p *Pool) pop(c int) (memory.Slice[byte], bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	stack := p.free[c]
	if len(stack) == 0 {
		return memory.Slice[byte]{}, false
	}
	s := stack[len(stack)-1]
	p.free[c] = stack[:len(stack)-1]
	p.retained -= s.Len()
	return s, true
}

// Put returns b to p, invalidating it. Putting a Buf back twice, or one with a
// size p never hands out, returns ErrForeign.
func (p *Pool) Put(b Buf) error {
	size := b.s.Len()
	c := classOf(size)
	if size == 0 {
		return fmt.Errorf("bufpool: put the zero Buf: %w", ErrForeign)
	}
	if c >= numClass {
		return memory.FreeSlice(p.alloc, b.s)
	}
	if size != 1<<(minLog+c) {
		return fmt.Errorf("bufpool: put a buffer of %d bytes: %w", size, ErrForeign)
	}
	p.mu.Lock()
	if memory.Checked && slices.Contains(p.free[c], b.s) {
		p.mu.Unlock()
		return fmt.Errorf("bufpool: put a buffer twice: %w", ErrForeign)
	}
	if p.retained+size > p.limit {
		p.mu.Unlock()
		return memory.FreeSlice(p.alloc, b.s)
	}
	p.free[c] = append(p.free[c], b.s)
	p.retained += size
	p.mu.Unlock()
	return nil
}

// Trim frees every retained buffer.
func (p *Pool) Trim() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	for c := range p.free {
		for _, s := range p.free[c] {
			errs = append(errs, memory.FreeSlice(p.alloc, s))
		}
		p.free[c] = nil
	}
	p.retained = 0
	return errors.Join(errs...)
}
