// Package btree provides an ordered map whose entries live off the Go heap.
//
// Fifteen entries share a node, so a lookup touches a few cache lines per level
// and each level multiplies reach sixteenfold. Leaves carry no child array: it
// is allocated separately and only for inner nodes, and most nodes are leaves.
package btree

import (
	"errors"
	"iter"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/seq"
	"github.com/stubbedev/memory-go/pool"
)

const (
	degree   = 8
	maxKeys  = 2*degree - 1
	minKeys  = degree - 1
	nodeGrow = 256
	kidsGrow = 64
)

type node[K, V any] struct {
	keys [maxKeys]K
	vals [maxKeys]V
	kids pool.Item[children[K, V]]
	n    int
}

type children[K, V any] struct {
	c [maxKeys + 1]pool.Item[node[K, V]]
}

// Tree is an ordered map with off-heap entries, sorted by the function given to
// New. It is not safe for concurrent use. Pointers from Ptr are valid until the
// next insertion or deletion.
type Tree[K, V any] struct {
	nodes *pool.Pool[node[K, V]]
	kids  *pool.Pool[children[K, V]]
	cmp   func(a, b K) int
	root  pool.Item[node[K, V]]
	n     int
	mods  uint
}

// New returns an empty Tree ordered by cmp, which returns a negative number when
// a sorts before b, zero when they are equal and a positive number otherwise.
func New[K, V any](a memory.Allocator, cmp func(a, b K) int) (*Tree[K, V], error) {
	nodes, err := pool.New[node[K, V]](a, nodeGrow)
	if err != nil {
		return nil, err
	}
	kids, err := pool.New[children[K, V]](a, kidsGrow)
	if err != nil {
		return nil, errors.Join(err, nodes.Free())
	}
	return &Tree[K, V]{nodes: nodes, kids: kids, cmp: cmp}, nil
}

// Len returns the number of entries.
func (t *Tree[K, V]) Len() int { return t.n }

func (t *Tree[K, V]) search(x *node[K, V], key K) (int, bool) {
	lo, hi := 0, x.n
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		switch c := t.cmp(x.keys[mid], key); {
		case c < 0:
			lo = mid + 1
		case c > 0:
			hi = mid
		default:
			return mid, true
		}
	}
	return lo, false
}

func child[K, V any](x *node[K, V], i int) *node[K, V] { return x.kids.Value().c[i].Value() }

func leaf[K, V any](x *node[K, V]) bool { return x.kids.IsNil() }

// Ptr returns a pointer to the value for key, or reports false if key is absent.
func (t *Tree[K, V]) Ptr(key K) (*V, bool) {
	if t.root.IsNil() {
		return nil, false
	}
	x := t.root.Value()
	for {
		i, found := t.search(x, key)
		if found {
			return &x.vals[i], true
		}
		if leaf(x) {
			return nil, false
		}
		x = child(x, i)
	}
}

// Get returns the value for key, or reports false if key is absent.
func (t *Tree[K, V]) Get(key K) (V, bool) {
	if p, ok := t.Ptr(key); ok {
		return *p, true
	}
	var zero V
	return zero, false
}

// Set stores value under key.
func (t *Tree[K, V]) Set(key K, value V) error {
	if t.root.IsNil() {
		root, err := t.nodes.Get()
		if err != nil {
			return err
		}
		t.root = root
	}
	if t.root.Value().n == maxKeys {
		if err := t.growRoot(); err != nil {
			return err
		}
	}
	x := t.root.Value()
	for {
		i, found := t.search(x, key)
		if found {
			x.vals[i] = value
			return nil
		}
		if leaf(x) {
			insertAt(x, i, key, value)
			t.n++
			t.mods++
			return nil
		}
		if child(x, i).n == maxKeys {
			if err := t.split(x, i); err != nil {
				return err
			}
			switch c := t.cmp(key, x.keys[i]); {
			case c == 0:
				x.vals[i] = value
				return nil
			case c > 0:
				i++
			}
		}
		x = child(x, i)
	}
}

func insertAt[K, V any](x *node[K, V], i int, key K, value V) {
	copy(x.keys[i+1:x.n+1], x.keys[i:x.n])
	copy(x.vals[i+1:x.n+1], x.vals[i:x.n])
	x.keys[i], x.vals[i] = key, value
	x.n++
}

func (t *Tree[K, V]) growRoot() error {
	top, err := t.nodes.Get()
	if err != nil {
		return err
	}
	kids, err := t.kids.Get()
	if err != nil {
		t.nodes.Put(top)
		return err
	}
	kids.Value().c[0] = t.root
	top.Value().kids = kids
	if err := t.split(top.Value(), 0); err != nil {
		t.kids.Put(kids)
		t.nodes.Put(top)
		return err
	}
	t.root = top
	return nil
}

func (t *Tree[K, V]) split(x *node[K, V], i int) error {
	left := child(x, i)
	rightItem, err := t.nodes.Get()
	if err != nil {
		return err
	}
	right := rightItem.Value()
	if !leaf(left) {
		kids, err := t.kids.Get()
		if err != nil {
			t.nodes.Put(rightItem)
			return err
		}
		right.kids = kids
		copy(kids.Value().c[:], left.kids.Value().c[degree:])
		clear(left.kids.Value().c[degree:])
	}
	copy(right.keys[:], left.keys[degree:])
	copy(right.vals[:], left.vals[degree:])
	right.n = minKeys
	sep, sepValue := left.keys[minKeys], left.vals[minKeys]
	clear(left.keys[minKeys:])
	clear(left.vals[minKeys:])
	left.n = minKeys
	c := x.kids.Value().c[:]
	copy(c[i+2:x.n+2], c[i+1:x.n+1])
	c[i+1] = rightItem
	insertAt(x, i, sep, sepValue)
	return nil
}

func removeAt[K, V any](x *node[K, V], i int) {
	copy(x.keys[i:x.n-1], x.keys[i+1:x.n])
	copy(x.vals[i:x.n-1], x.vals[i+1:x.n])
	x.n--
	var (
		zeroK K
		zeroV V
	)
	x.keys[x.n], x.vals[x.n] = zeroK, zeroV
}

// Delete removes key and reports whether it was present.
func (t *Tree[K, V]) Delete(key K) bool {
	if t.root.IsNil() {
		return false
	}
	removed := t.remove(t.root.Value(), key)
	root := t.root.Value()
	if root.n == 0 && !leaf(root) {
		next := root.kids.Value().c[0]
		t.kids.Put(root.kids)
		t.nodes.Put(t.root)
		t.root = next
	}
	if removed {
		t.n--
		t.mods++
	}
	return removed
}

func (t *Tree[K, V]) remove(x *node[K, V], key K) bool {
	for {
		i, found := t.search(x, key)
		if leaf(x) {
			if found {
				removeAt(x, i)
			}
			return found
		}
		if found {
			left, right := child(x, i), child(x, i+1)
			switch {
			case left.n > minKeys:
				last := left
				for !leaf(last) {
					last = child(last, last.n)
				}
				x.keys[i], x.vals[i] = last.keys[last.n-1], last.vals[last.n-1]
				key, x = x.keys[i], left
			case right.n > minKeys:
				first := right
				for !leaf(first) {
					first = child(first, 0)
				}
				x.keys[i], x.vals[i] = first.keys[0], first.vals[0]
				key, x = x.keys[i], right
			default:
				t.merge(x, i)
				x = left
			}
			continue
		}
		if child(x, i).n == minKeys {
			i = t.fill(x, i)
		}
		x = child(x, i)
	}
}

func (t *Tree[K, V]) fill(x *node[K, V], i int) int {
	switch {
	case i > 0 && child(x, i-1).n > minKeys:
		rotateRight(x, i-1)
		return i
	case i < x.n && child(x, i+1).n > minKeys:
		rotateLeft(x, i)
		return i
	case i < x.n:
		t.merge(x, i)
		return i
	default:
		t.merge(x, i-1)
		return i - 1
	}
}

func rotateRight[K, V any](x *node[K, V], i int) {
	left, right := child(x, i), child(x, i+1)
	insertAt(right, 0, x.keys[i], x.vals[i])
	x.keys[i], x.vals[i] = left.keys[left.n-1], left.vals[left.n-1]
	if !leaf(left) {
		c := right.kids.Value().c[:]
		copy(c[1:right.n+1], c[:right.n])
		c[0] = left.kids.Value().c[left.n]
		left.kids.Value().c[left.n] = pool.Item[node[K, V]]{}
	}
	removeAt(left, left.n-1)
}

func rotateLeft[K, V any](x *node[K, V], i int) {
	left, right := child(x, i), child(x, i+1)
	insertAt(left, left.n, x.keys[i], x.vals[i])
	x.keys[i], x.vals[i] = right.keys[0], right.vals[0]
	if !leaf(right) {
		c := right.kids.Value().c[:]
		left.kids.Value().c[left.n] = c[0]
		copy(c[:right.n], c[1:right.n+1])
		c[right.n] = pool.Item[node[K, V]]{}
	}
	removeAt(right, 0)
}

func (t *Tree[K, V]) merge(x *node[K, V], i int) {
	left, right := child(x, i), child(x, i+1)
	rightItem := x.kids.Value().c[i+1]
	left.keys[left.n], left.vals[left.n] = x.keys[i], x.vals[i]
	copy(left.keys[left.n+1:], right.keys[:right.n])
	copy(left.vals[left.n+1:], right.vals[:right.n])
	if !leaf(left) {
		copy(left.kids.Value().c[left.n+1:], right.kids.Value().c[:right.n+1])
		t.kids.Put(right.kids)
	}
	left.n += right.n + 1
	c := x.kids.Value().c[:]
	copy(c[i+1:x.n], c[i+2:x.n+1])
	c[x.n] = pool.Item[node[K, V]]{}
	removeAt(x, i)
	t.nodes.Put(rightItem)
}

// Min returns the smallest key and its value, or reports false if t is empty.
func (t *Tree[K, V]) Min() (K, V, bool) {
	var (
		zeroK K
		zeroV V
	)
	if t.n == 0 {
		return zeroK, zeroV, false
	}
	x := t.root.Value()
	for !leaf(x) {
		x = child(x, 0)
	}
	return x.keys[0], x.vals[0], true
}

// Max returns the largest key and its value, or reports false if t is empty.
func (t *Tree[K, V]) Max() (K, V, bool) {
	var (
		zeroK K
		zeroV V
	)
	if t.n == 0 {
		return zeroK, zeroV, false
	}
	x := t.root.Value()
	for !leaf(x) {
		x = child(x, x.n)
	}
	return x.keys[x.n-1], x.vals[x.n-1], true
}

// All yields every entry in ascending key order. Updating a value during the
// loop is allowed; adding or deleting keys panics.
func (t *Tree[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		t.start(yield, nil)
	}
}

// From yields the entries with keys at or above from, in ascending key order,
// seeking directly to the first. Updating a value during the loop is allowed;
// adding or deleting keys panics.
func (t *Tree[K, V]) From(from K) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		t.start(yield, &from)
	}
}

// Range yields the entries with keys in [from, to), in ascending key order, under the
// same rules as From.
func (t *Tree[K, V]) Range(from, to K) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range t.From(from) {
			if t.cmp(k, to) >= 0 || !yield(k, v) {
				return
			}
		}
	}
}

// Backward yields every entry in descending key order, under the same rules as All.
func (t *Tree[K, V]) Backward() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if t.n != 0 {
			t.walkBack(t.root.Value(), t.watched(yield))
		}
	}
}

// Keys yields every key in ascending order, under the same rules as All.
func (t *Tree[K, V]) Keys() iter.Seq[K] { return seq.Keys(t.All()) }

// Values yields every value in ascending key order, under the same rules as All.
func (t *Tree[K, V]) Values() iter.Seq[V] { return seq.Values(t.All()) }

func (t *Tree[K, V]) watched(yield func(K, V) bool) func(K, V) bool {
	mods := t.mods
	return func(k K, v V) bool {
		if !yield(k, v) {
			return false
		}
		if t.mods != mods {
			panic("btree: modified during All")
		}
		return true
	}
}

func (t *Tree[K, V]) start(yield func(K, V) bool, from *K) {
	if t.n != 0 {
		t.walk(t.root.Value(), t.watched(yield), from)
	}
}

func (t *Tree[K, V]) walkBack(x *node[K, V], yield func(K, V) bool) bool {
	isLeaf := leaf(x)
	for i := x.n; ; i-- {
		if !isLeaf && !t.walkBack(child(x, i), yield) {
			return false
		}
		if i == 0 {
			return true
		}
		if !yield(x.keys[i-1], x.vals[i-1]) {
			return false
		}
	}
}

func (t *Tree[K, V]) walk(x *node[K, V], yield func(K, V) bool, from *K) bool {
	isLeaf := leaf(x)
	i := 0
	descend := !isLeaf
	if from != nil {
		at, found := t.search(x, *from)
		i, descend = at, !isLeaf && !found
	}
	if descend && !t.walk(child(x, i), yield, from) {
		return false
	}
	for ; i < x.n; i++ {
		if !yield(x.keys[i], x.vals[i]) {
			return false
		}
		if !isLeaf && !t.walk(child(x, i+1), yield, nil) {
			return false
		}
	}
	return true
}

// Free releases every entry. t must not be used afterwards.
func (t *Tree[K, V]) Free() error {
	t.root, t.n = pool.Item[node[K, V]]{}, 0
	t.mods++
	return errors.Join(t.nodes.Free(), t.kids.Free())
}
