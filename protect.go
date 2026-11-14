package memory

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

var (
	// ErrFrozen reports a change to memory that Freeze or FreezeSlice made
	// read-only.
	ErrFrozen = errors.New("memory is frozen")
	// ErrSharesPages reports an attempt to protect an allocation small enough to
	// share its pages with other allocations.
	ErrSharesPages = errors.New("allocation shares its pages")
)

func protectVerb(frozen bool) string {
	if frozen {
		return "freeze"
	}
	return "thaw"
}

func directOf(a Allocator) (*direct, error) {
	switch a := a.(type) {
	case *Local:
		return &a.d, nil
	case *Shared:
		return &a.d, nil
	case *Arena:
		return directOf(a.parent)
	case *Faulty:
		return directOf(a.a)
	}
	return nil, fmt.Errorf("memory: %T cannot protect allocations: %w", a, errors.ErrUnsupported)
}

func checkResizable(a Allocator, p unsafe.Pointer, size int) error {
	d, err := directOf(a)
	if err != nil {
		return nil
	}
	return d.check(p, size)
}

func (d *direct) protect(p unsafe.Pointer, size int, frozen bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.verify(p, size)
	if m.frozen == frozen {
		return nil
	}
	if err := osProtect(p, m.reserved, !frozen); err != nil {
		return err
	}
	m.frozen = frozen
	d.live[p] = m
	return nil
}

// FreezeSlice makes the memory of s read-only, so that writing to it faults
// instead of corrupting it. Only allocations over 8 KiB own their pages; smaller
// ones return ErrSharesPages. Resizing a frozen Slice returns ErrFrozen, and
// freeing it thaws it first. Populate must not be called on a frozen Slice. s
// must come from a and be live, or FreezeSlice panics.
func FreezeSlice[T any](a Allocator, s Slice[T]) error { return protectSlice(a, s, true) }

// ThawSlice makes the memory of a frozen s writable again. Thawing a Slice that
// is not frozen does nothing.
func ThawSlice[T any](a Allocator, s Slice[T]) error { return protectSlice(a, s, false) }

func protectSlice[T any](a Allocator, s Slice[T], frozen bool) error {
	verb := protectVerb(frozen)
	size := sizeOf[T](s.n)
	if size <= smallMax {
		return fmt.Errorf("memory: %s %d×%s: %d bytes, and allocations of at most %d bytes share their pages: %w",
			verb, s.n, reflect.TypeFor[T](), size, smallMax, ErrSharesPages)
	}
	d, err := directOf(a)
	if err != nil {
		return err
	}
	if err := d.protect(unsafe.Pointer(s.p), size, frozen); err != nil {
		return fmt.Errorf("memory: %s %d×%s at %#x: %w", verb, s.n, reflect.TypeFor[T](), uintptr(unsafe.Pointer(s.p)), err)
	}
	return nil
}

// Freeze makes the elements of r read-only, so that writing to them faults
// instead of corrupting them. Grow, Truncate and Populate return ErrFrozen until
// Thaw. Freezing a frozen Region does nothing.
func (r *Region[T]) Freeze() error { return r.protect(true) }

// Thaw makes the elements of a frozen r writable again. Thawing a Region that is
// not frozen does nothing.
func (r *Region[T]) Thaw() error { return r.protect(false) }

// Frozen reports whether r is frozen.
func (r *Region[T]) Frozen() bool { return r.frozen }

func (r *Region[T]) protect(frozen bool) error {
	if r.frozen == frozen {
		return nil
	}
	start, size := pageSpan(r.start(), r.lead()+r.committed)
	if size > 0 && r.committed > 0 {
		if err := osProtect(start, size, !frozen); err != nil {
			return fmt.Errorf("memory: %s region of %d bytes at %#x: %w", protectVerb(frozen), size, uintptr(start), err)
		}
	}
	r.frozen = frozen
	return nil
}

func (r *Region[T]) unfrozen(op string) error {
	if r.frozen {
		return fmt.Errorf("memory: %s region of %d: %w", op, r.n, ErrFrozen)
	}
	return nil
}
