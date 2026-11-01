// Package hashmap provides hash maps whose entries live off the Go heap.
//
// Map is keyed by any comparable pointer-free type, and Strings by text. Both
// use a Swiss table: groups of eight slots with one control byte each, so a
// u64-to-u64 map costs 17 bytes per slot at up to 7/8 load. Keys hash with
// hash/maphash unless a Map is given a faster Hasher.
package hashmap

import (
	"hash/maphash"
	"iter"
	"math/bits"
	"math/rand/v2"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/swiss"
)

const fold = 0x9e3779b97f4a7c15

// Integer is the set of key types Int hashes.
type Integer interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// Hasher hashes a key under a seed chosen per Map. Equal keys must hash equal.
type Hasher[K any] func(seed uint64, key K) uint64

// Int is a Hasher for integer keys, about five times faster than the default.
// It is not collision resistant, so an attacker who controls keys and learns
// the seed could degrade a Map; use the default for untrusted keys.
func Int[K Integer](seed uint64, key K) uint64 {
	hi, lo := bits.Mul64(uint64(key)^seed, fold)
	return hi ^ lo
}

type slot[K comparable, V any] struct {
	key   K
	value V
}

// Map is a hash map with off-heap entries. It is not safe for concurrent use.
// Iteration order is unspecified. Pointers into it are valid until the next
// call that may grow or shrink it.
type Map[K comparable, V any] struct {
	table *swiss.Table[slot[K, V]]
	hash  func(K) uint64
}

// New returns an empty Map with room for capacity entries, hashing with
// hash/maphash.
func New[K comparable, V any](a memory.Allocator, capacity int) (*Map[K, V], error) {
	seed := maphash.MakeSeed()
	return build[K, V](a, capacity, func(key K) uint64 { return maphash.Comparable(seed, key) })
}

// NewHashed returns an empty Map with room for capacity entries, hashing with h
// under a random seed.
func NewHashed[K comparable, V any](a memory.Allocator, capacity int, h Hasher[K]) (*Map[K, V], error) {
	seed := rand.Uint64() //nolint:gosec // resists accidental clustering, not attackers; see Int
	return build[K, V](a, capacity, func(key K) uint64 { return h(seed, key) })
}

func build[K comparable, V any](a memory.Allocator, capacity int, hash func(K) uint64) (*Map[K, V], error) {
	table, err := swiss.New(a, capacity, func(s *slot[K, V]) uint64 { return hash(s.key) })
	if err != nil {
		return nil, err
	}
	return &Map[K, V]{table: table, hash: hash}, nil
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int { return m.table.Len() }

func (m *Map[K, V]) find(key K, h uint64) *slot[K, V] {
	return m.table.Find(h, func(s *slot[K, V]) bool { return s.key == key })
}

// Get returns the value for key, or reports false if key is absent.
func (m *Map[K, V]) Get(key K) (V, bool) {
	if s := m.find(key, m.hash(key)); s != nil {
		return s.value, true
	}
	var zero V
	return zero, false
}

// Ptr returns a pointer to the value for key, or reports false if key is
// absent.
func (m *Map[K, V]) Ptr(key K) (*V, bool) {
	s := m.find(key, m.hash(key))
	if s == nil {
		return nil, false
	}
	return &s.value, true
}

// Set stores value under key.
func (m *Map[K, V]) Set(key K, value V) error {
	h := m.hash(key)
	s := m.find(key, h)
	if s == nil {
		var err error
		if s, err = m.table.Insert(h); err != nil {
			return err
		}
	}
	*s = slot[K, V]{key: key, value: value}
	return nil
}

// Clip shrinks the table to the smallest size that holds the current entries.
func (m *Map[K, V]) Clip() error { return m.table.Clip() }

// Delete removes key and reports whether it was present.
func (m *Map[K, V]) Delete(key K) bool {
	_, ok := m.table.Remove(m.hash(key), func(s *slot[K, V]) bool { return s.key == key })
	return ok
}

// All yields every key and value. Updating the value of an existing key
// during the loop is allowed; adding or deleting keys, or Clip, panics.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.table.All("hashmap", func(s *slot[K, V]) bool { return yield(s.key, s.value) })
	}
}

// Keys yields every key.
func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range m.All() {
			if !yield(k) {
				return
			}
		}
	}
}

// Values yields every value.
func (m *Map[K, V]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range m.All() {
			if !yield(v) {
				return
			}
		}
	}
}

// Free releases the entries. m must not be used afterwards.
func (m *Map[K, V]) Free() error { return m.table.Free() }
