package memory

import (
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// ErrRegionFull reports growth past the length a Region reserved.
var ErrRegionFull = errors.New("region is full")

const (
	maxRegionLen = math.MaxUint32 - 1
	commitStep   = 1 << 20
)

// Index refers to an element of a Region in four bytes, half the size of a
// pointer, and may be stored off-heap. It stays meaningful across processes and
// mappings because it is relative to the Region. The zero Index is nil.
type Index[T any] uint32

// IsNil reports whether i refers to nothing.
func (i Index[T]) IsNil() bool { return i == 0 }

// Region is one contiguous array of up to a fixed number of off-heap Ts that
// grows in place: it reserves address space up front and only uses memory for
// the elements it has, so pointers into it stay valid until Truncate drops
// their element or Close. It is not safe for concurrent use, and its elements
// are not protected by the misuse guards beyond bounds checks.
type Region[T any] struct {
	base      *T
	limit     int
	n         int
	reserved  int
	committed int
}

// NewRegion reserves address space for maxLen Ts without using memory for them.
// maxLen may be far above what the machine could hold, and is at most
// 2³²−2 so that every element has an Index.
func NewRegion[T any](maxLen int) (*Region[T], error) {
	size, err := byteSize[T](maxLen)
	if err != nil {
		return nil, err
	}
	if int64(maxLen) > maxRegionLen {
		return nil, fmt.Errorf("memory: region of %d elements: %w", maxLen, ErrTooLarge)
	}
	reserved, err := pageRound(size)
	if err != nil {
		return nil, fmt.Errorf("memory: region of %d elements: %w", maxLen, err)
	}
	align := osPageSize
	if reserved >= hugeSize {
		align = hugeSize
	}
	base, err := osReserve(reserved, align)
	if err != nil {
		return nil, fmt.Errorf("memory: reserve %d bytes: %w", reserved, err)
	}
	return &Region[T]{base: (*T)(base), limit: maxLen, reserved: reserved}, nil
}

// UseHugePages asks the operating system to back r with huge pages. See
// Local.UseHugePages for the trade-offs.
func (r *Region[T]) UseHugePages() error {
	if err := osAdviseHuge(unsafe.Pointer(r.base), r.reserved); err != nil {
		return fmt.Errorf("memory: advise huge pages for %d bytes: %w", r.reserved, err)
	}
	return nil
}

// Len returns the number of elements.
func (r *Region[T]) Len() int { return r.n }

// Cap returns the length r can grow to.
func (r *Region[T]) Cap() int { return r.limit }

// Elems returns the elements as a Go slice.
func (r *Region[T]) Elems() []T { return unsafe.Slice(r.base, r.n) }

// Grow appends n zeroed elements and returns the Index of the first. It does
// not move existing elements.
func (r *Region[T]) Grow(n int) (Index[T], error) {
	if n < 0 {
		return 0, fmt.Errorf("memory: grow region by %d: %w", n, ErrNegativeLen)
	}
	if n > r.limit-r.n {
		return 0, fmt.Errorf("memory: grow region of %d by %d past %d: %w", r.n, n, r.limit, ErrRegionFull)
	}
	if err := r.commit(sizeOf[T](r.n + n)); err != nil {
		return 0, err
	}
	first := r.n
	r.n += n
	return r.IndexAt(first), nil
}

func (r *Region[T]) commit(bytes int) error {
	if bytes <= r.committed {
		return nil
	}
	need, err := pageRound(bytes)
	if err != nil {
		return fmt.Errorf("memory: commit %d bytes: %w", bytes, err)
	}
	target := min(r.reserved, max(need, r.committed+commitStep))
	at := unsafe.Add(unsafe.Pointer(r.base), r.committed)
	if err := osRecommit(at, target-r.committed); err != nil {
		return fmt.Errorf("memory: commit %d bytes at %#x: %w", target-r.committed, uintptr(at), err)
	}
	r.committed = target
	return nil
}

// IndexAt returns the Index of element i. i may equal Len, naming the element
// the next Grow creates. It panics if i is out of range.
func (r *Region[T]) IndexAt(i int) Index[T] {
	if i < 0 || i > r.n {
		panic(fmt.Sprintf("memory: region index out of range [%d] with length %d", i, r.n))
	}
	return Index[T](i + 1) //nolint:gosec // limit bounds i below MaxUint32
}

// Ptr returns a pointer to the element i refers to. It panics if i is nil or
// past the end.
func (r *Region[T]) Ptr(i Index[T]) *T {
	if i == 0 || int(i) > r.n {
		panic(fmt.Sprintf("memory: region index %d invalid with length %d", i, r.n))
	}
	var zero T
	return (*T)(unsafe.Add(unsafe.Pointer(r.base), (int(i)-1)*int(unsafe.Sizeof(zero))))
}

// Truncate shortens r to n elements. Dropped elements read as zero if r grows
// again, and memory beyond a small slack goes back to the operating system. It
// panics if n is out of range.
func (r *Region[T]) Truncate(n int) error {
	if n < 0 || n > r.n {
		panic(fmt.Sprintf("memory: truncate region of %d to %d", r.n, n))
	}
	var zero T
	size := int(unsafe.Sizeof(zero))
	from, to := n*size, r.n*size
	r.n = n
	if to == from {
		return nil
	}
	boundary, err := pageRound(from)
	if err != nil {
		return fmt.Errorf("memory: truncate to %d bytes: %w", from, err)
	}
	if to-boundary <= hotBytes {
		clearFrom(unsafe.Pointer(r.base), from, to)
		return nil
	}
	clearFrom(unsafe.Pointer(r.base), from, boundary)
	at := unsafe.Add(unsafe.Pointer(r.base), boundary)
	if err := osDecommit(at, r.committed-boundary); err != nil {
		return fmt.Errorf("memory: decommit %d bytes at %#x: %w", r.committed-boundary, uintptr(at), err)
	}
	r.committed = boundary
	return nil
}

// Footprint reports the memory r holds from the operating system.
func (r *Region[T]) Footprint() Footprint {
	return Footprint{Mapped: r.reserved, Committed: r.committed}
}

// Close releases r, invalidating every pointer into it. Closing twice does
// nothing.
func (r *Region[T]) Close() error {
	if r.base == nil {
		return nil
	}
	err := unmap(unsafe.Pointer(r.base), r.reserved)
	*r = Region[T]{}
	if err != nil {
		return fmt.Errorf("memory: close region: %w", err)
	}
	return nil
}
