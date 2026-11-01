// Package swiss is the open-addressed table behind the module's hash maps:
// groups of eight slots, each with one control byte holding seven bits of the
// hash, probed a group at a time with word-at-a-time comparisons. It needs no
// stored hash and reaches a 7/8 load, so it costs one byte per slot.
package swiss

import (
	"fmt"
	"math"
	"math/bits"

	memory "github.com/stubbedev/memory-go"
)

const (
	groupSlots  = 8
	groupGrowth = 7
	lsbs        = 0x0101010101010101
	msbs        = 0x8080808080808080
	empties     = 0x8080808080808080
	ctrlEmpty   = 0x80
	ctrlDeleted = 0xfe
	h2Bits      = 7
	h2Mask      = 1<<h2Bits - 1
)

type group[S any] struct {
	ctrl  uint64
	slots [groupSlots]S
}

func matchH2(ctrl, h2 uint64) uint64 {
	x := ctrl ^ (lsbs * h2)
	return (x - lsbs) &^ x & msbs
}

func matchEmpty(ctrl uint64) uint64 { return ctrl &^ (ctrl << 6) & msbs }

func matchFree(ctrl uint64) uint64 { return ctrl & msbs }

func slotOf(match uint64) uint64 { return uint64(bits.TrailingZeros64(match)) >> 3 }

// Table is an open-addressed table of slots. It is not safe for concurrent
// use. Pointers into it are valid until the next call that may grow or shrink it.
type Table[S any] struct {
	alloc      memory.Allocator
	hash       func(*S) uint64
	store      memory.Slice[group[S]]
	groups     []group[S]
	mask       uint64
	n          int
	growthLeft int
	mods       uint
}

func groupsFor(entries int) (int, error) {
	switch {
	case entries < 0:
		return 0, fmt.Errorf("swiss: capacity %d: %w", entries, memory.ErrNegativeLen)
	case entries > math.MaxInt/2:
		return 0, fmt.Errorf("swiss: capacity %d: %w", entries, memory.ErrTooLarge)
	case entries <= groupGrowth:
		return 1, nil
	}
	return 1 << bits.Len(uint((entries+groupGrowth-1)/groupGrowth-1)), nil
}

// New returns an empty Table with room for capacity entries. hash must return
// the hash the slot was inserted under.
func New[S any](a memory.Allocator, capacity int, hash func(*S) uint64) (*Table[S], error) {
	count, err := groupsFor(capacity)
	if err != nil {
		return nil, err
	}
	t := &Table[S]{alloc: a, hash: hash}
	if err := t.rehash(count); err != nil {
		return nil, err
	}
	return t, nil
}

// Len returns the number of entries.
func (t *Table[S]) Len() int { return t.n }

func (t *Table[S]) rehash(count int) error {
	store, err := memory.MakeSlice[group[S]](t.alloc, count)
	if err != nil {
		return err
	}
	fresh := store.Elems()
	for i := range fresh {
		fresh[i].ctrl = empties
	}
	old, oldGroups := t.store, t.groups
	t.store, t.groups, t.mask = store, fresh, uint64(count)-1
	t.growthLeft = count*groupGrowth - t.n
	t.mods++
	for i := range oldGroups {
		g := &oldGroups[i]
		for m := ^g.ctrl & msbs; m != 0; m &= m - 1 {
			at := slotOf(m)
			h := t.hash(&g.slots[at])
			into, slot := t.firstFree(h)
			into.setCtrl(slot, h&h2Mask)
			into.slots[slot] = g.slots[at]
		}
	}
	return memory.FreeSlice(t.alloc, old)
}

func (g *group[S]) setCtrl(slot, value uint64) {
	shift := slot * 8
	g.ctrl = g.ctrl&^(0xff<<shift) | value<<shift
}

func (t *Table[S]) firstFree(h uint64) (*group[S], uint64) {
	at := (h >> h2Bits) & t.mask
	for step := uint64(1); ; step++ {
		g := &t.groups[at]
		if m := matchFree(g.ctrl); m != 0 {
			return g, slotOf(m)
		}
		at = (at + step) & t.mask
	}
}

// Find returns the slot under hash h for which eq reports true, or nil.
func (t *Table[S]) Find(h uint64, eq func(*S) bool) *S {
	g, slot := t.locate(h, eq)
	if g == nil {
		return nil
	}
	return &g.slots[slot]
}

func (t *Table[S]) locate(h uint64, eq func(*S) bool) (*group[S], uint64) {
	h2 := h & h2Mask
	at := (h >> h2Bits) & t.mask
	for step := uint64(1); ; step++ {
		g := &t.groups[at]
		for m := matchH2(g.ctrl, h2); m != 0; m &= m - 1 {
			slot := slotOf(m)
			if eq(&g.slots[slot]) {
				return g, slot
			}
		}
		if matchEmpty(g.ctrl) != 0 {
			return nil, 0
		}
		at = (at + step) & t.mask
	}
}

// Insert returns a slot to fill for hash h, whose key must be absent.
func (t *Table[S]) Insert(h uint64) (*S, error) {
	g, slot := t.firstFree(h)
	reusesTombstone := (g.ctrl>>(slot*8))&0xff == ctrlDeleted
	if !reusesTombstone && t.growthLeft == 0 {
		if err := t.makeRoom(); err != nil {
			return nil, err
		}
		g, slot = t.firstFree(h)
		reusesTombstone = false
	}
	if !reusesTombstone {
		t.growthLeft--
	}
	g.setCtrl(slot, h&h2Mask)
	t.n++
	t.mods++
	return &g.slots[slot], nil
}

func (t *Table[S]) makeRoom() error {
	if capacity := len(t.groups) * groupGrowth; t.n <= capacity/2 {
		return t.rehash(len(t.groups))
	}
	return t.rehash(2 * len(t.groups))
}

// Remove deletes the slot under hash h for which eq reports true and returns
// it, or reports false.
func (t *Table[S]) Remove(h uint64, eq func(*S) bool) (S, bool) {
	var zero S
	g, slot := t.locate(h, eq)
	if g == nil {
		return zero, false
	}
	removed := g.slots[slot]
	g.slots[slot] = zero
	if matchEmpty(g.ctrl) != 0 {
		g.setCtrl(slot, ctrlEmpty)
		t.growthLeft++
	} else {
		g.setCtrl(slot, ctrlDeleted)
	}
	t.n--
	t.mods++
	return removed, true
}

// Clip shrinks the table to the smallest size that holds the current entries.
func (t *Table[S]) Clip() error {
	count, err := groupsFor(t.n)
	if err != nil {
		return err
	}
	if count >= len(t.groups) {
		return nil
	}
	return t.rehash(count)
}

// All calls yield with every slot until it returns false. A change to the set
// of entries during the loop panics, reporting who as the modified container.
func (t *Table[S]) All(who string, yield func(*S) bool) {
	mods := t.mods
	for i := range t.groups {
		g := &t.groups[i]
		for m := ^g.ctrl & msbs; m != 0; m &= m - 1 {
			if !yield(&g.slots[slotOf(m)]) {
				return
			}
			if t.mods != mods {
				panic(who + ": modified during All")
			}
		}
	}
}

// Free releases the table. t must not be used afterwards.
func (t *Table[S]) Free() error {
	err := memory.FreeSlice(t.alloc, t.store)
	t.store, t.groups, t.mask, t.n, t.growthLeft = memory.Slice[group[S]]{}, nil, 0, 0, 0
	t.mods++
	return err
}
