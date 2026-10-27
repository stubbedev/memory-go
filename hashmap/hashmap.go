// Package hashmap provides a hash map whose entries live off the Go heap.
//
// It uses open addressing with linear probing and backward-shift deletion,
// hashing keys with hash/maphash. Each slot stores its key's hash, so probes
// compare hashes before keys and resizing never rehashes.
package hashmap

import (
	"fmt"
	"hash/maphash"
	"iter"
	"math"
	"math/bits"

	memory "github.com/stubbedev/memory-go"
)

const (
	minLog   = 3
	occupied = 1 << 63
)

type slot[K comparable, V any] struct {
	hash  uint64
	key   K
	value V
}

// Map is a hash map with off-heap entries. It is not safe for concurrent use.
// Iteration order is unspecified. Pointers into it are valid until the next
// call that may grow or shrink it.
type Map[K comparable, V any] struct {
	alloc memory.Allocator
	seed  maphash.Seed
	slots memory.Slice[slot[K, V]]
	mask  uint64
	n     int
}

// New returns an empty Map with room for capacity entries.
func New[K comparable, V any](a memory.Allocator, capacity int) (*Map[K, V], error) {
	log, err := slotsLog(capacity)
	if err != nil {
		return nil, err
	}
	m := &Map[K, V]{alloc: a, seed: maphash.MakeSeed()}
	if err := m.rehash(log); err != nil {
		return nil, err
	}
	return m, nil
}

func slotsLog(entries int) (int, error) {
	switch {
	case entries < 0:
		return 0, fmt.Errorf("hashmap: capacity %d: %w", entries, memory.ErrNegativeLen)
	case entries > math.MaxInt/4:
		return 0, fmt.Errorf("hashmap: capacity %d: %w", entries, memory.ErrTooLarge)
	}
	need := (4*entries + 2) / 3
	if need <= 1<<minLog {
		return minLog, nil
	}
	return bits.Len(uint(need - 1)), nil
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int { return m.n }

func (m *Map[K, V]) hash(key K) uint64 { return maphash.Comparable(m.seed, key) | occupied }

func (m *Map[K, V]) find(key K, h uint64) (uint64, bool) {
	s := m.slots.Elems()
	i := h & m.mask
	for s[i].hash != 0 {
		if s[i].hash == h && s[i].key == key {
			return i, true
		}
		i = (i + 1) & m.mask
	}
	return i, false
}

// Get returns the value for key, or reports false if key is absent.
func (m *Map[K, V]) Get(key K) (V, bool) {
	if p, ok := m.Ptr(key); ok {
		return *p, true
	}
	var zero V
	return zero, false
}

// Ptr returns a pointer to the value for key, or reports false if key is
// absent.
func (m *Map[K, V]) Ptr(key K) (*V, bool) {
	i, ok := m.find(key, m.hash(key))
	if !ok {
		return nil, false
	}
	return &m.slots.Elems()[i].value, true
}

// Set stores value under key.
func (m *Map[K, V]) Set(key K, value V) error {
	h := m.hash(key)
	i, ok := m.find(key, h)
	if !ok && 4*(m.n+1) > 3*m.slots.Len() {
		if err := m.rehash(bits.Len64(m.mask) + 1); err != nil {
			return err
		}
		i, _ = m.find(key, h)
	}
	if !ok {
		m.n++
	}
	m.slots.Elems()[i] = slot[K, V]{hash: h, key: key, value: value}
	return nil
}

// Clip shrinks the table to the smallest size that holds the current entries.
func (m *Map[K, V]) Clip() error {
	log, err := slotsLog(m.n)
	if err != nil {
		return err
	}
	if log >= bits.Len64(m.mask) {
		return nil
	}
	return m.rehash(log)
}

func (m *Map[K, V]) rehash(log int) error {
	slots, err := memory.MakeSlice[slot[K, V]](m.alloc, 1<<log)
	if err != nil {
		return err
	}
	old := m.slots
	m.slots, m.mask = slots, uint64(1)<<log-1
	s := slots.Elems()
	for _, e := range old.Elems() {
		if e.hash == 0 {
			continue
		}
		i := e.hash & m.mask
		for s[i].hash != 0 {
			i = (i + 1) & m.mask
		}
		s[i] = e
	}
	return memory.FreeSlice(m.alloc, old)
}

// Delete removes key and reports whether it was present.
func (m *Map[K, V]) Delete(key K) bool {
	hole, ok := m.find(key, m.hash(key))
	if !ok {
		return false
	}
	s := m.slots.Elems()
	for i := (hole + 1) & m.mask; s[i].hash != 0; i = (i + 1) & m.mask {
		if (i-s[i].hash)&m.mask >= (i-hole)&m.mask {
			s[hole] = s[i]
			hole = i
		}
	}
	s[hole] = slot[K, V]{}
	m.n--
	return true
}

// All yields every key and value.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, e := range m.slots.Elems() {
			if e.hash != 0 && !yield(e.key, e.value) {
				return
			}
		}
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
func (m *Map[K, V]) Free() error {
	err := memory.FreeSlice(m.alloc, m.slots)
	m.slots, m.mask, m.n = memory.Slice[slot[K, V]]{}, 0, 0
	return err
}
