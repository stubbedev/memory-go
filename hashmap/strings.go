package hashmap

import (
	"errors"
	"hash/maphash"
	"iter"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/swiss"
)

type stringSlot[V any] struct {
	key   memory.String
	value V
}

// Strings is a hash map from text to V with both keys and values off-heap, so
// it costs the garbage collector nothing however many entries it holds, unlike
// a map[string]V. Lookups do not allocate. It is not safe for concurrent use.
// Iteration order is unspecified. Pointers into it are valid until the next
// call that may grow or shrink it.
type Strings[V any] struct {
	alloc memory.Allocator
	seed  maphash.Seed
	table *swiss.Table[stringSlot[V]]
}

// NewStrings returns an empty Strings with room for capacity entries.
func NewStrings[V any](a memory.Allocator, capacity int) (*Strings[V], error) {
	seed := maphash.MakeSeed()
	table, err := swiss.New(a, capacity, func(s *stringSlot[V]) uint64 { return maphash.String(seed, s.key.String()) })
	if err != nil {
		return nil, err
	}
	return &Strings[V]{alloc: a, seed: seed, table: table}, nil
}

// Len returns the number of entries.
func (m *Strings[V]) Len() int { return m.table.Len() }

func (m *Strings[V]) find(key string, h uint64) *stringSlot[V] {
	return m.table.Find(h, func(s *stringSlot[V]) bool { return s.key.String() == key })
}

// Get returns the value for key, or reports false if key is absent.
func (m *Strings[V]) Get(key string) (V, bool) {
	if s := m.find(key, maphash.String(m.seed, key)); s != nil {
		return s.value, true
	}
	var zero V
	return zero, false
}

// GetBytes is Get for a key held as bytes, without converting it to a string.
func (m *Strings[V]) GetBytes(key []byte) (V, bool) {
	return m.Get(unsafe.String(unsafe.SliceData(key), len(key))) //nolint:gosec // lookup only reads the key
}

// Ptr returns a pointer to the value for key, or reports false if key is
// absent.
func (m *Strings[V]) Ptr(key string) (*V, bool) {
	s := m.find(key, maphash.String(m.seed, key))
	if s == nil {
		return nil, false
	}
	return &s.value, true
}

// Set stores value under key, copying key off-heap if it is new.
func (m *Strings[V]) Set(key string, value V) error {
	h := maphash.String(m.seed, key)
	if s := m.find(key, h); s != nil {
		s.value = value
		return nil
	}
	copied, err := memory.NewString(m.alloc, key)
	if err != nil {
		return err
	}
	s, err := m.table.Insert(h)
	if err != nil {
		return errors.Join(err, memory.FreeString(m.alloc, copied))
	}
	*s = stringSlot[V]{key: copied, value: value}
	return nil
}

// Delete removes key and reports whether it was present, or returns an error if
// releasing the key failed.
func (m *Strings[V]) Delete(key string) (bool, error) {
	removed, ok := m.table.Remove(maphash.String(m.seed, key), func(s *stringSlot[V]) bool { return s.key.String() == key })
	if !ok {
		return false, nil
	}
	return true, memory.FreeString(m.alloc, removed.key)
}

// Clip shrinks the table to the smallest size that holds the current entries.
func (m *Strings[V]) Clip() error { return m.table.Clip() }

// All yields every key and value. The keys read off-heap memory and are valid
// until deleted; clone them with strings.Clone to keep them longer. Updating
// the value of an existing key during the loop is allowed; adding or deleting
// keys, or Clip, panics.
func (m *Strings[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		m.table.All("hashmap", func(s *stringSlot[V]) bool { return yield(s.key.String(), s.value) })
	}
}

// Free releases the keys and entries. m must not be used afterwards.
func (m *Strings[V]) Free() error {
	var errs []error
	m.table.All("hashmap", func(s *stringSlot[V]) bool {
		errs = append(errs, memory.FreeString(m.alloc, s.key))
		return true
	})
	errs = append(errs, m.table.Free())
	return errors.Join(errs...)
}
