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
	"github.com/stubbedev/memory-go/internal/seq"
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

// Map is a hash map with off-heap entries. It is not safe for concurrent use.
// Iteration order is unspecified. Pointers into it are valid until the next
// call that may grow or shrink it.
type Map[K comparable, V any] struct {
	table   *swiss.Table[swiss.Entry[K, V]]
	hash    func(K) uint64
	touched uint64
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
	table, err := swiss.New(a, capacity, func(e *swiss.Entry[K, V]) uint64 { return hash(e.Key) })
	if err != nil {
		return nil, err
	}
	return &Map[K, V]{table: table, hash: hash}, nil
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int { return m.table.Len() }

func (m *Map[K, V]) find(key K, h uint64) *swiss.Entry[K, V] {
	return swiss.FindKey(m.table, h, key)
}

// Get returns the value for key, or reports false if key is absent.
func (m *Map[K, V]) Get(key K) (V, bool) {
	if e := m.find(key, m.hash(key)); e != nil {
		return e.Value, true
	}
	var zero V
	return zero, false
}

// Contains reports whether key is present.
func (m *Map[K, V]) Contains(key K) bool { return m.find(key, m.hash(key)) != nil }

// Ptr returns a pointer to the value for key, or reports false if key is
// absent.
func (m *Map[K, V]) Ptr(key K) (*V, bool) {
	e := m.find(key, m.hash(key))
	if e == nil {
		return nil, false
	}
	return &e.Value, true
}

// Set stores value under key.
func (m *Map[K, V]) Set(key K, value V) error {
	h := m.hash(key)
	if e := m.find(key, h); e != nil {
		*e = swiss.Entry[K, V]{Key: key, Value: value}
		return nil
	}
	return m.insert(key, h, value)
}

func (m *Map[K, V]) insert(key K, h uint64, value V) error {
	e, err := m.table.Insert(h)
	if err != nil {
		return err
	}
	*e = swiss.Entry[K, V]{Key: key, Value: value}
	return nil
}

// GetOrSet returns the value for key and true if key is present. Otherwise it stores
// value and returns it and false, hashing key only once either way.
func (m *Map[K, V]) GetOrSet(key K, value V) (V, bool, error) {
	h := m.hash(key)
	if e := m.find(key, h); e != nil {
		return e.Value, true, nil
	}
	return value, false, m.insert(key, h, value)
}

// Upsert stores f(old, true) under key if key is present, and f(zero, false)
// otherwise, hashing key only once. f must not modify m.
func (m *Map[K, V]) Upsert(key K, f func(old V, found bool) V) error {
	h := m.hash(key)
	if e := m.find(key, h); e != nil {
		e.Value = f(e.Value, true)
		return nil
	}
	var zero V
	return m.insert(key, h, f(zero, false))
}

// Clear removes every entry, keeping the memory for reuse.
func (m *Map[K, V]) Clear() { m.table.Clear() }

// Clip shrinks the table to the smallest size that holds the current entries.
func (m *Map[K, V]) Clip() error { return m.table.Clip() }

// Delete removes key and reports whether it was present.
func (m *Map[K, V]) Delete(key K) bool {
	_, ok := m.table.Remove(m.hash(key), func(e *swiss.Entry[K, V]) bool { return e.Key == key })
	return ok
}

// All yields every key and value. Updating the value of an existing key
// during the loop is allowed; adding or deleting keys, or Clip, panics.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		m.table.All("hashmap", func(e *swiss.Entry[K, V]) bool { return yield(e.Key, e.Value) })
	}
}

// Keys yields every key.
func (m *Map[K, V]) Keys() iter.Seq[K] { return seq.Keys(m.All()) }

// Values yields every value.
func (m *Map[K, V]) Values() iter.Seq[V] { return seq.Values(m.All()) }

// Free releases the entries. m must not be used afterwards.
func (m *Map[K, V]) Free() error { return m.table.Free() }

const batchSize = 16

// GetMany looks up every key, storing each value in values and whether it was found
// in found, which must be at least as long as keys. On a table larger than the CPU
// caches it beats calling Get in a loop, because it starts each batch's cache misses
// together rather than one after another.
func (m *Map[K, V]) GetMany(keys []K, values []V, found []bool) {
	values, found = values[:len(keys)], found[:len(keys)]
	var hashes [batchSize]uint64
	for start := 0; start < len(keys); start += batchSize {
		batch := keys[start:min(start+batchSize, len(keys))]
		var touched uint64
		for i, key := range batch {
			hashes[i] = m.hash(key)
			touched += m.table.Touch(hashes[i])
		}
		m.touched = touched
		for i, key := range batch {
			e := m.find(key, hashes[i])
			if e == nil {
				var zero V
				values[start+i], found[start+i] = zero, false
				continue
			}
			values[start+i], found[start+i] = e.Value, true
		}
	}
}
