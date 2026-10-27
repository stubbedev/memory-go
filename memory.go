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
		checkLive(unsafe.Pointer(p.p))
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
	checkLive(unsafe.Pointer(s.p))
	return unsafe.Slice(s.p, s.n)
}

// Ref returns a non-owning reference to element i, valid until s is resized
// or freed. It panics if i is out of range or s is already freed.
func (s Slice[T]) Ref(i int) Ref[T] { return Ref[T]{p: &s.Elems()[i]} }

// New allocates a zeroed T.
func New[T any](a Allocator) (Ptr[T], error) {
	size, err := byteSize[T](1)
	if err != nil {
		return Ptr[T]{}, err
	}
	p, err := a.calloc(size)
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
	size, err := byteSize[T](n)
	if err != nil || n == 0 {
		return Slice[T]{}, err
	}
	p, err := a.calloc(size)
	if err != nil {
		return Slice[T]{}, fmt.Errorf("memory: allocate %d×%s: %w", n, reflect.TypeFor[T](), err)
	}
	return Slice[T]{p: (*T)(p), n: n}, nil
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
	r := Slice[T]{p: (*T)(p), n: n}
	if n > s.n {
		clear(r.Elems()[s.n:])
	}
	return r, err
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
