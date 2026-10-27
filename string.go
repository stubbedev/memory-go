package memory

import "unsafe"

// String owns off-heap UTF-8 bytes. Unlike a Go string it may be stored
// off-heap. FreeString releases it. The zero String is empty.
type String struct{ b Slice[byte] }

// NewString copies s off-heap.
func NewString(a Allocator, s string) (String, error) {
	return AppendString(a, String{}, s)
}

// AppendString appends more to s, returning the grown String. Errors follow
// ResizeSlice.
func AppendString(a Allocator, s String, more string) (String, error) {
	if more == "" {
		return s, nil
	}
	n := s.b.Len()
	b, err := ResizeSlice(a, s.b, n+len(more))
	if b == s.b {
		return s, err
	}
	copy(b.Elems()[n:], more)
	return String{b: b}, err
}

// FreeString releases s.
func FreeString(a Allocator, s String) error { return FreeSlice(a, s.b) }

// Len returns the length of s in bytes.
func (s String) Len() int { return s.b.Len() }

// String returns s as a Go string without copying. The result reads freed
// memory once s is freed or appended to, so copy it with strings.Clone to
// keep it longer. It panics if s is already freed.
func (s String) String() string {
	b := s.b.Elems()
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// Bytes returns the bytes of s, valid until s is freed or appended to. It
// panics if s is already freed.
func (s String) Bytes() []byte { return s.b.Elems() }
