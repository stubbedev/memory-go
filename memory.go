package memory

import (
	"fmt"
	"reflect"
	"unsafe"
)

// Ptr owns a single off-heap T allocated by New. Free releases it. The zero
// Ptr is nil.
type Ptr[T any] struct{ p *T }

func (Ptr[T]) offHeap() {}

// Value returns a Go pointer to the T, valid until it is freed. It panics if
// the T is already freed.
func (p Ptr[T]) Value() *T {
	if p.p != nil {
		checkLive(unsafe.Pointer(p.p), sizeOf[T](1))
	}
	return p.p
}

// IsNil reports whether p refers to nothing.
func (p Ptr[T]) IsNil() bool { return p.p == nil }

// Ref returns a non-owning reference to the T. It panics if the T is already
// freed.
func (p Ptr[T]) Ref() Ref[T] { return Ref[T]{p: p.Value()} }

// Ref is a non-owning reference to an off-heap T, such as an element of a
// Slice or a value in a typedarena. It cannot be freed on its own. The zero
// Ref is nil.
type Ref[T any] struct{ p *T }

func (Ref[T]) offHeap() {}

// Value returns a Go pointer to the T, valid as long as its owner is.
func (r Ref[T]) Value() *T { return r.p }

// IsNil reports whether r refers to nothing.
func (r Ref[T]) IsNil() bool { return r.p == nil }

// Slice owns n consecutive off-heap Ts allocated by MakeSlice or ResizeSlice.
// FreeSlice releases them. The zero Slice is empty.
type Slice[T any] struct {
	p *T
	n int
}

func (Slice[T]) offHeap() {}

// Len returns the number of elements.
func (s Slice[T]) Len() int { return s.n }

// Elems returns the elements as a Go slice, valid until s is resized or
// freed. Its capacity equals its length, so append copies to the Go heap. It
// panics if s is already freed.
func (s Slice[T]) Elems() []T {
	if s.n == 0 {
		return nil
	}
	checkLive(unsafe.Pointer(s.p), sizeOf[T](s.n))
	return unsafe.Slice(s.p, s.n)
}

// Ref returns a non-owning reference to element i, valid until s is resized
// or freed. It panics if i is out of range or s is already freed.
func (s Slice[T]) Ref(i int) Ref[T] {
	if uint(i) >= uint(s.n) {
		panic(fmt.Sprintf("memory: index out of range [%d] with length %d", i, s.n))
	}
	checkLive(unsafe.Pointer(s.p), sizeOf[T](s.n))
	return Ref[T]{p: (*T)(unsafe.Add(unsafe.Pointer(s.p), uintptr(i)*unsafe.Sizeof(*s.p)))}
}

// New allocates a zeroed T.
func New[T any](a Allocator) (Ptr[T], error) {
	size, err := byteSize[T](1)
	if err != nil {
		return Ptr[T]{}, err
	}
	p, err := a.alloc(size, alignOf[T](), zeroed)
	if err != nil {
		return Ptr[T]{}, fmt.Errorf("memory: allocate %s: %w", reflect.TypeFor[T](), err)
	}
	return Ptr[T]{p: (*T)(p)}, nil
}

// Free releases p, which must come from New on the same allocator. Freeing a
// nil Ptr does nothing. Freeing p twice, or through another allocator,
// panics.
func Free[T any](a Allocator, p Ptr[T]) error {
	if p.IsNil() {
		return nil
	}
	if err := a.free(unsafe.Pointer(p.p), sizeOf[T](1)); err != nil {
		return fmt.Errorf("memory: free %s: %w", reflect.TypeFor[T](), err)
	}
	return nil
}

// MakeSlice allocates n zeroed Ts. n == 0 returns the empty Slice.
func MakeSlice[T any](a Allocator, n int) (Slice[T], error) {
	return makeSlice[T](a, n, alignOf[T](), zeroed)
}

// ResizeSlice changes the length of s to n, keeping the common prefix and
// zeroing any new elements. On error s is left intact, unless only returning
// the old memory to the operating system failed: then the resized Slice comes
// back along with the error, and s is freed. n == 0 frees s. s must
// come from the same allocator and be live, or ResizeSlice panics.
func ResizeSlice[T any](a Allocator, s Slice[T], n int) (Slice[T], error) {
	if s.n == 0 {
		return MakeSlice[T](a, n)
	}
	size, err := byteSize[T](n)
	if err != nil {
		return s, err
	}
	if n == 0 {
		return Slice[T]{}, FreeSlice(a, s)
	}
	p, err := a.realloc(unsafe.Pointer(s.p), sizeOf[T](s.n), size)
	if err != nil {
		err = fmt.Errorf("memory: resize %d×%s to %d: %w", s.n, reflect.TypeFor[T](), n, err)
	}
	if p == nil {
		return s, err
	}
	return Slice[T]{p: (*T)(p), n: n}, err
}

// FreeSlice releases s. Freeing the empty Slice does nothing. Freeing s twice,
// or through another allocator, panics.
func FreeSlice[T any](a Allocator, s Slice[T]) error {
	if s.n == 0 {
		return nil
	}
	if err := a.free(unsafe.Pointer(s.p), sizeOf[T](s.n)); err != nil {
		return fmt.Errorf("memory: free %d×%s: %w", s.n, reflect.TypeFor[T](), err)
	}
	return nil
}

// View is a non-owning handle to consecutive off-heap Ts, such as part of a
// Slice. It cannot be freed on its own. The zero View is empty.
type View[T any] struct {
	p *T
	n int
}

func (View[T]) offHeap() {}

// Len returns the number of elements.
func (v View[T]) Len() int { return v.n }

// Elems returns the elements as a Go slice, valid as long as their owner is.
// Its capacity equals its length.
func (v View[T]) Elems() []T { return unsafe.Slice(v.p, v.n) }

// View returns a non-owning handle to elements [from, to), valid until s is
// resized or freed. It panics if the range is out of bounds or s is already
// freed.
func (s Slice[T]) View(from, to int) View[T] {
	e := s.Elems()[from:to:to]
	if len(e) == 0 {
		return View[T]{}
	}
	return View[T]{p: &e[0], n: len(e)}
}

// NewUninit is New without zeroing: the T holds whatever the memory held. Use
// it only for a value that is written in full before it is read. A T holding
// handles is zeroed anyway, because the garbage collector inspects the old value of
// a handle while it is overwritten and would take stale bytes for a pointer.
func NewUninit[T any](a Allocator) (Ptr[T], error) {
	size, err := byteSize[T](1)
	if err != nil {
		return Ptr[T]{}, err
	}
	p, err := a.alloc(size, alignOf[T](), uninitFill[T]())
	if err != nil {
		return Ptr[T]{}, fmt.Errorf("memory: allocate %s: %w", reflect.TypeFor[T](), err)
	}
	return Ptr[T]{p: (*T)(p)}, nil
}

// MakeSliceUninit is MakeSlice without zeroing: the elements hold whatever the
// memory held, and a parser or decoder that overwrites them skips paying for
// the clear. n == 0 returns the empty Slice. A T holding handles is zeroed anyway; see
// NewUninit.
func MakeSliceUninit[T any](a Allocator, n int) (Slice[T], error) {
	return makeSlice[T](a, n, alignOf[T](), uninitFill[T]())
}

// MakeSliceAligned allocates n zeroed Ts whose first element's address is a
// multiple of align, for hardware and system interfaces that need it: cache
// lines, SIMD registers, direct I/O. align must be a power of two no larger than
// 4 KiB, and allocations over 8 KiB start on an operating system page regardless.
// The alignment does not survive ResizeSlice unless the new size also gets it.
func MakeSliceAligned[T any](a Allocator, n, align int) (Slice[T], error) {
	return makeSliceAligned[T](a, n, align, zeroed)
}

// MakeSliceAlignedUninit is MakeSliceAligned without zeroing, for a buffer about to
// be overwritten. A T holding handles is zeroed anyway; see NewUninit.
func MakeSliceAlignedUninit[T any](a Allocator, n, align int) (Slice[T], error) {
	return makeSliceAligned[T](a, n, align, uninitFill[T]())
}

func makeSliceAligned[T any](a Allocator, n, align int, f fill) (Slice[T], error) {
	if align <= 0 || align&(align-1) != 0 || align > maxAlign {
		return Slice[T]{}, fmt.Errorf("memory: alignment %d: %w", align, ErrAlignment)
	}
	return makeSlice[T](a, n, max(align, alignOf[T]()), f)
}

// NewFrom allocates a T holding value.
func NewFrom[T any](a Allocator, value T) (Ptr[T], error) {
	p, err := NewUninit[T](a)
	if err != nil {
		return Ptr[T]{}, err
	}
	*p.p = value
	return p, nil
}

// SliceFrom allocates a Slice holding a copy of values. Empty values return the
// empty Slice.
func SliceFrom[T any](a Allocator, values []T) (Slice[T], error) {
	s, err := MakeSliceUninit[T](a, len(values))
	if err != nil {
		return Slice[T]{}, err
	}
	copy(s.Elems(), values)
	return s, nil
}

func makeSlice[T any](a Allocator, n, align int, f fill) (Slice[T], error) {
	size, err := byteSize[T](n)
	if err != nil || n == 0 {
		return Slice[T]{}, err
	}
	p, err := a.alloc(size, align, f)
	if err != nil {
		return Slice[T]{}, fmt.Errorf("memory: allocate %d×%s: %w", n, reflect.TypeFor[T](), err)
	}
	return Slice[T]{p: (*T)(p), n: n}, nil
}
