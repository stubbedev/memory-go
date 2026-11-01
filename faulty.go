package memory

import (
	"errors"
	"unsafe"
)

// ErrInjected is the failure a Faulty allocator reports.
var ErrInjected = errors.New("injected failure")

const neverFail = -1

// Faulty is an Allocator for tests that fails on demand, because out-of-memory and
// unmap failures cannot otherwise be provoked. Everything else goes to the wrapped
// allocator. It is not safe for concurrent use.
type Faulty struct {
	a      Allocator
	allocs int
	frees  int
}

// NewFaulty returns a Faulty over a that does not fail until told to.
func NewFaulty(a Allocator) *Faulty {
	return &Faulty{a: a, allocs: neverFail, frees: neverFail}
}

// FailAllocsAfter lets n more allocations succeed, then fails every later one, along
// with every resize that does not fit in place, leaving their input intact. A
// negative n stops failing.
func (f *Faulty) FailAllocsAfter(n int) { f.allocs = n }

// FailFreesAfter lets n more frees succeed. Each later free still releases the memory
// but reports an error, as does each later resize, which returns the new memory with
// it, as when the operating system refuses to take the old memory back. A negative n
// stops failing.
func (f *Faulty) FailFreesAfter(n int) { f.frees = n }

func spend(budget *int) bool {
	switch {
	case *budget < 0:
		return true
	case *budget == 0:
		return false
	}
	*budget--
	return true
}

func (f *Faulty) alloc(size, align int, fl fill) (unsafe.Pointer, error) {
	if !spend(&f.allocs) {
		return nil, ErrInjected
	}
	return f.a.alloc(size, align, fl)
}

func (f *Faulty) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	if size > oldSize && !spend(&f.allocs) {
		return nil, ErrInjected
	}
	q, err := f.a.realloc(p, oldSize, size)
	if q != nil && err == nil && !spend(&f.frees) {
		return q, ErrInjected
	}
	return q, err
}

func (f *Faulty) free(p unsafe.Pointer, size int) error {
	err := f.a.free(p, size)
	if err == nil && !spend(&f.frees) {
		return ErrInjected
	}
	return err
}

// Stats reports the wrapped allocator's Stats.
func (f *Faulty) Stats() Stats { return f.a.Stats() }

// Footprint reports the wrapped allocator's Footprint.
func (f *Faulty) Footprint() Footprint { return f.a.Footprint() }
