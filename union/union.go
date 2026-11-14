// Package union stores values of several concrete types in one off-heap
// handle, the way an interface does on the Go heap. Off-heap memory cannot
// hold an interface, because an interface holds Go pointers; a Value holds a
// small type ID and the bytes of the concrete value instead, and a Registry
// maps the ID back to the type.
//
// Every registered type must pass the same check as an allocation: no Go
// pointers, though memory handles such as memory.String are allowed. A
// payload of up to 16 bytes is kept inside the Value; a larger one is
// allocated from a memory.Allocator and released by Free.
package union

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
)

// InlineSize is the largest payload, in bytes, kept inside a Value without
// allocating.
const InlineSize = 16

// ErrUnregistered reports a type that was never registered with the Registry.
var ErrUnregistered = errors.New("type not registered")

// Value holds one value of a registered type, or nothing. It holds no Go
// pointers, so it may be stored off-heap: in memory.New, a vector or a hashmap.
// A Value whose payload exceeds InlineSize owns an allocation that Free
// releases. The zero Value holds nothing. It is not comparable, because ==
// could only compare addresses of large payloads; compare what Load returns.
type Value struct {
	_      [0]func()
	inline [InlineSize]byte
	big    memory.Slice[byte]
	id     uint32
}

// ID returns the type ID of the value v holds, or 0 if v holds nothing.
func (v Value) ID() uint32 { return v.id }

// IsNil reports whether v holds nothing.
func (v Value) IsNil() bool { return v.id == 0 }

func (v *Value) payload(size uintptr) []byte {
	if size > InlineSize {
		return v.big.Elems()
	}
	return v.inline[:size]
}

type entry struct {
	typ   reflect.Type
	size  uintptr
	boxed func(payload []byte) any
	store func(a memory.Allocator, id uint32, x any) (Value, error)
}

type table struct {
	entries []entry
	ids     map[reflect.Type]uint32
}

// Registry assigns type IDs, starting at 1, to the concrete types a Value may
// hold. The zero Registry is empty and ready. It is safe for concurrent use and
// must not be copied after use. IDs belong to one Registry: a Value must be
// read with the Registry that stored it.
type Registry struct {
	mu    sync.Mutex
	table atomic.Pointer[table]
}

func (r *Registry) entry(id uint32) (entry, bool) {
	t := r.table.Load()
	if t == nil || id == 0 || uint64(id) > uint64(len(t.entries)) {
		return entry{}, false
	}
	return t.entries[id-1], true
}

func (r *Registry) id(typ reflect.Type) (uint32, bool) {
	t := r.table.Load()
	if t == nil {
		return 0, false
	}
	id, ok := t.ids[typ]
	return id, ok
}

// Register adds T to r and returns its type ID. Registering a type again
// returns the ID it already has. T must hold no Go pointers; otherwise the
// error wraps memory.ErrHoldsPointers.
func Register[T any](r *Registry) (uint32, error) {
	typ := reflect.TypeFor[T]()
	if id, ok := r.id(typ); ok {
		return id, nil
	}
	if err := memory.CheckType[T](); err != nil {
		return 0, fmt.Errorf("union: register %s: %w", typ, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.table.Load()
	if old == nil {
		old = &table{}
	}
	if id, ok := old.ids[typ]; ok {
		return id, nil
	}
	if uint64(len(old.entries)) >= math.MaxUint32 {
		return 0, fmt.Errorf("union: register %s: %d types: %w", typ, len(old.entries), memory.ErrTooLarge)
	}
	next := &table{
		entries: append(slices.Clip(old.entries), entry{
			typ:  typ,
			size: unsafe.Sizeof(*new(T)),
			boxed: func(payload []byte) any {
				return decode[T](payload)
			},
			store: func(a memory.Allocator, id uint32, x any) (Value, error) {
				t, _ := x.(T)
				return store(a, id, t)
			},
		}),
		ids: make(map[reflect.Type]uint32, len(old.ids)+1),
	}
	maps.Copy(next.ids, old.ids)
	id := uint32(len(next.entries)) //nolint:gosec // bounded above
	next.ids[typ] = id
	r.table.Store(next)
	return id, nil
}

func bytesOf[T any](p *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(p)), unsafe.Sizeof(*p))
}

func decode[T any](payload []byte) T {
	var x T
	copy(bytesOf(&x), payload)
	return x
}

// Store returns a Value holding x, allocating from a if x is larger than
// InlineSize. T must be registered with r; otherwise the error wraps
// ErrUnregistered.
func Store[T any](a memory.Allocator, r *Registry, x T) (Value, error) {
	id, ok := r.id(reflect.TypeFor[T]())
	if !ok {
		return Value{}, fmt.Errorf("union: store %s: %w", reflect.TypeFor[T](), ErrUnregistered)
	}
	return store(a, id, x)
}

// StoreInterface is Store for a value whose concrete type is known only at run
// time, such as one held in an interface. Its dynamic type must be registered
// with r; otherwise, or if x is nil, the error wraps ErrUnregistered.
func StoreInterface(a memory.Allocator, r *Registry, x any) (Value, error) {
	id, ok := r.id(reflect.TypeOf(x))
	if !ok {
		return Value{}, fmt.Errorf("union: store %T: %w", x, ErrUnregistered)
	}
	e, _ := r.entry(id)
	return e.store(a, id, x)
}

func store[T any](a memory.Allocator, id uint32, x T) (Value, error) {
	v := Value{id: id}
	src := bytesOf(&x)
	if len(src) <= InlineSize {
		copy(v.inline[:], src)
		return v, nil
	}
	big, err := memory.SliceFrom(a, src)
	if err != nil {
		return Value{}, fmt.Errorf("union: store %s: %w", reflect.TypeFor[T](), err)
	}
	v.big = big
	return v, nil
}

// Load returns the T v holds, or reports false if v holds nothing or a value
// of another type. It panics if the payload of v was already freed.
func Load[T any](r *Registry, v Value) (T, bool) {
	e, ok := r.entry(v.id)
	if !ok || e.typ != reflect.TypeFor[T]() {
		var zero T
		return zero, false
	}
	return decode[T](v.payload(e.size)), true
}

// Interface returns the value v holds as an interface, or nil if v holds
// nothing or an ID r does not know. It panics if the payload of v was already
// freed.
func Interface(r *Registry, v Value) any {
	e, ok := r.entry(v.id)
	if !ok {
		return nil
	}
	return e.boxed(v.payload(e.size))
}

// TypeOf returns the type of the value v holds, or nil if v holds nothing or
// an ID r does not know.
func TypeOf(r *Registry, v Value) reflect.Type {
	e, ok := r.entry(v.id)
	if !ok {
		return nil
	}
	return e.typ
}

// Free releases the payload of v, which must come from Store with the same
// allocator. Freeing a Value with an inline payload, or the zero Value, does
// nothing. Freeing a large payload twice, or through another allocator, panics.
func Free(a memory.Allocator, v Value) error {
	if err := memory.FreeSlice(a, v.big); err != nil {
		return fmt.Errorf("union: free: %w", err)
	}
	return nil
}
