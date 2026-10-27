// Package hashmap provides an open-addressing hash map whose entries live off the Go heap.
package hashmap

import (
	"hash/maphash"
	"iter"

	memory "github.com/stubbedev/memory-go"
)

const minSlots = 8

type slot[K comparable, V any] struct {
	key   K
	value V
	used  bool
}

type Map[K comparable, V any] struct {
	alloc memory.Allocator
	seed  maphash.Seed
	slots []slot[K, V]
	mask  uint64
	n     int
}

func New[K comparable, V any](a memory.Allocator) (*Map[K, V], error) {
	slots, err := memory.MakeSlice[slot[K, V]](a, minSlots)
	if err != nil {
		return nil, err
	}
	return &Map[K, V]{alloc: a, seed: maphash.MakeSeed(), slots: slots, mask: minSlots - 1}, nil
}

func (m *Map[K, V]) Len() int { return m.n }

func (m *Map[K, V]) home(key K) uint64 { return maphash.Comparable(m.seed, key) & m.mask }

func (m *Map[K, V]) next(i uint64) uint64 { return (i + 1) & m.mask }

func (m *Map[K, V]) distance(from, to uint64) uint64 { return (to - from) & m.mask }

func (m *Map[K, V]) find(key K) (uint64, bool) {
	i := m.home(key)
	for m.slots[i].used {
		if m.slots[i].key == key {
			return i, true
		}
		i = m.next(i)
	}
	return i, false
}

func (m *Map[K, V]) Get(key K) (V, bool) {
	if p, ok := m.Ptr(key); ok {
		return *p, true
	}
	var zero V
	return zero, false
}

func (m *Map[K, V]) Ptr(key K) (*V, bool) {
	i, ok := m.find(key)
	if !ok {
		return nil, false
	}
	return &m.slots[i].value, true
}

func (m *Map[K, V]) Set(key K, value V) error {
	i, ok := m.find(key)
	if !ok && 4*(m.n+1) > 3*len(m.slots) {
		if err := m.grow(); err != nil {
			return err
		}
		i, _ = m.find(key)
	}
	if !ok {
		m.n++
	}
	m.slots[i] = slot[K, V]{key: key, value: value, used: true}
	return nil
}

func (m *Map[K, V]) grow() error {
	slots, err := memory.MakeSlice[slot[K, V]](m.alloc, 2*len(m.slots))
	if err != nil {
		return err
	}
	old := m.slots
	m.slots, m.mask = slots, m.mask<<1|1
	for _, s := range old {
		if s.used {
			i, _ := m.find(s.key)
			m.slots[i] = s
		}
	}
	return memory.FreeSlice(m.alloc, old)
}

func (m *Map[K, V]) Delete(key K) bool {
	hole, ok := m.find(key)
	if !ok {
		return false
	}
	for i := m.next(hole); m.slots[i].used; i = m.next(i) {
		if m.distance(m.home(m.slots[i].key), i) >= m.distance(hole, i) {
			m.slots[hole] = m.slots[i]
			hole = i
		}
	}
	m.slots[hole] = slot[K, V]{}
	m.n--
	return true
}

func (m *Map[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, s := range m.slots {
			if s.used && !yield(s.key, s.value) {
				return
			}
		}
	}
}

func (m *Map[K, V]) Keys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for k := range m.All() {
			if !yield(k) {
				return
			}
		}
	}
}

func (m *Map[K, V]) Values() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range m.All() {
			if !yield(v) {
				return
			}
		}
	}
}

func (m *Map[K, V]) Free() error {
	err := memory.FreeSlice(m.alloc, m.slots)
	m.slots, m.n = nil, 0
	return err
}
