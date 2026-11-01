// Package intern stores each distinct string once, off the Go heap, and names it
// with a 4-byte Symbol. Repeated text, such as field names or log labels, then
// costs one copy, and comparing two Symbols is comparing two integers.
package intern

import (
	"errors"
	"fmt"
	"hash/maphash"
	"math"
	"unsafe"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/chunks"
	"github.com/stubbedev/memory-go/internal/swiss"
	"github.com/stubbedev/memory-go/vector"
)

const textChunk = 64 << 10

// Symbol names an interned string in four bytes and may be stored off-heap. The
// zero Symbol is nil.
type Symbol uint32

// Table holds interned strings until Free. It is not safe for concurrent use.
type Table struct {
	seed  maphash.Seed
	text  *chunks.Chunks[byte]
	names *vector.Vector[memory.View[byte]]
	table *swiss.Table[Symbol]
}

// New returns an empty Table.
func New(a memory.Allocator) (*Table, error) {
	text, err := chunks.New[byte](a, textChunk)
	if err != nil {
		return nil, err
	}
	names, err := vector.New[memory.View[byte]](a, 0)
	if err != nil {
		return nil, errors.Join(err, text.Free())
	}
	t := &Table{seed: maphash.MakeSeed(), text: text, names: names}
	t.table, err = swiss.New(a, 0, func(s *Symbol) uint64 { return maphash.String(t.seed, t.Text(*s)) })
	if err != nil {
		return nil, errors.Join(err, names.Free(), text.Free())
	}
	return t, nil
}

// Len returns the number of distinct strings.
func (t *Table) Len() int { return t.names.Len() }

// Text returns the string s names. The result reads off-heap memory and is valid
// until Free. It panics if s is nil or from another Table with more strings.
func (t *Table) Text(s Symbol) string {
	b := t.names.At(int(s) - 1).Elems()
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func (t *Table) find(text string, h uint64) *Symbol {
	return t.table.Find(h, func(s *Symbol) bool { return t.Text(*s) == text })
}

// Lookup returns the Symbol of text, or reports false if it was never interned.
func (t *Table) Lookup(text string) (Symbol, bool) {
	if s := t.find(text, maphash.String(t.seed, text)); s != nil {
		return *s, true
	}
	return 0, false
}

// Intern returns the Symbol of text, storing a copy the first time it is seen.
func (t *Table) Intern(text string) (Symbol, error) {
	h := maphash.String(t.seed, text)
	if s := t.find(text, h); s != nil {
		return *s, nil
	}
	if int64(t.names.Len()) >= math.MaxUint32-1 {
		return 0, fmt.Errorf("intern: %d strings: %w", t.names.Len(), memory.ErrTooLarge)
	}
	if err := t.names.Reserve(t.names.Len() + 1); err != nil {
		return 0, err
	}
	view, err := t.text.TakeView(len(text))
	if err != nil {
		return 0, err
	}
	copy(view.Elems(), text)
	slot, err := t.table.Insert(h)
	if err != nil {
		return 0, err
	}
	if err := t.names.Push(view); err != nil {
		return 0, err
	}
	sym := Symbol(t.names.Len()) //nolint:gosec // bounded above
	*slot = sym
	return sym, nil
}

// Free releases every string. t must not be used afterwards.
func (t *Table) Free() error {
	return errors.Join(t.table.Free(), t.names.Free(), t.text.Free())
}
