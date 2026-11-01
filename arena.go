package memory

import (
	"errors"
	"fmt"
	"slices"
	"unsafe"
)

const bumpData = sideOffset + sideEntry + wordAlign - (sideOffset+sideEntry)%wordAlign

func (s *span) isBump() bool { return s.slot == 0 }

func (s *span) holds(p unsafe.Pointer, size int) bool {
	off := uint32(uintptr(p) & spanMask)
	return off >= s.data && off+uint32(size) <= s.carved
}

// Arena is an Allocator that hands out memory by bumping an offset and frees it
// all at once with Reset, or back to a Mark with Release. It takes spans from a
// parent allocator and takes mixed types and every container in this module, so
// a request's whole object graph can go away in one call.
//
// It suits one goroutine at a time, and spans it holds count in its own Footprint,
// not the parent's. Closing the parent invalidates it. Freeing or resizing the most recent
// allocation reclaims its space or grows it in place; freeing anything else does
// nothing until Reset. Reading a handle past the current mark panics, but a
// handle whose space has been handed out again is not caught, and a double free
// is not detected. The zero Arena is not usable; use NewArena.
type Arena struct {
	_      noCopy
	g      guard
	parent spanSource
	owner  uint32
	spans  []*span
	cur    int
	big    []bigAlloc
	last   unsafe.Pointer
	stats  Stats
}

type bigAlloc struct {
	p    unsafe.Pointer
	size int
}

// Mark is a position in an Arena to Release back to.
type Mark struct {
	span, carved int
	big          int
	stats        Stats
	valid        bool
}

type spanSource interface {
	Allocator
	takeSpan() (*span, error)
	giveSpan(*span) error
}

// NewArena returns an empty Arena drawing from a, which must be a Local or a
// Shared.
func NewArena(a Allocator) (*Arena, error) {
	src, ok := a.(spanSource)
	if !ok {
		return nil, fmt.Errorf("memory: arena on %T: %w", a, ErrArenaParent)
	}
	return &Arena{parent: src, owner: nextOwner()}, nil
}

func (a *Arena) newSpan() (*span, error) {
	s, err := a.parent.takeSpan()
	if err != nil {
		return nil, err
	}
	dirty := s.dirty
	*s = span{data: bumpData, carved: bumpData, dirty: max(dirty, bumpData), owner: a.owner}
	if Checked {
		*(*uint16)(unsafe.Add(unsafe.Pointer(s), sideOffset)) = 0
	}
	return s, nil
}

func (a *Arena) current() (*span, error) {
	if a.cur < len(a.spans) {
		return a.spans[a.cur], nil
	}
	s, err := a.newSpan()
	if err != nil {
		return nil, err
	}
	a.spans = append(a.spans, s)
	return s, nil
}

func (a *Arena) alloc(size, align int, f fill) (unsafe.Pointer, error) {
	a.g.enter()
	defer a.g.exit()
	if size > smallMax {
		p, err := a.parent.alloc(size, align, f)
		if err != nil {
			return nil, err
		}
		a.big = append(a.big, bigAlloc{p: p, size: size})
		return p, nil
	}
	return a.bump(size, align, f)
}

func (a *Arena) bump(size, align int, f fill) (unsafe.Pointer, error) {
	s, err := a.current()
	if err != nil {
		return nil, err
	}
	off := alignUp(s.carved, uint32(align))
	if off+uint32(size) > spanSize {
		a.cur++
		if s, err = a.current(); err != nil {
			return nil, err
		}
		s.carved = s.data
		off = alignUp(s.carved, uint32(align))
	}
	end := off + uint32(size)
	if f == zeroed && off < s.dirty {
		clear(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(s), off)), min(end, s.dirty)-off))
	}
	s.carved = end
	s.dirty = max(s.dirty, end)
	a.stats.Live++
	a.stats.Bytes += size
	a.last = unsafe.Add(unsafe.Pointer(s), off)
	return a.last, nil
}

func alignUp(v, align uint32) uint32 { return (v + align - 1) &^ (align - 1) }

func (a *Arena) verify(s *span, p unsafe.Pointer, size int) {
	if Checked && (s.owner != a.owner || !s.isBump()) {
		panic(fmt.Sprintf("memory: %#x belongs to a different allocator", uintptr(p)))
	}
	if Checked && !s.holds(p, size) {
		panic(fmt.Sprintf("memory: %#x is not a live allocation: already freed, or never allocated here", uintptr(p)))
	}
}

func (a *Arena) realloc(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	a.g.enter()
	defer a.g.exit()
	if oldSize <= smallMax {
		s := spanOf(p)
		a.verify(s, p, oldSize)
		off := uint32(uintptr(p) & spanMask)
		if p == a.last && size <= smallMax && off+uint32(size) <= spanSize {
			end := off + uint32(size)
			if end > s.carved && s.carved < s.dirty {
				clear(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(s), s.carved)), min(end, s.dirty)-s.carved))
			}
			s.carved = end
			s.dirty = max(s.dirty, end)
			a.stats.Bytes += size - oldSize
			return p, nil
		}
	} else if size > smallMax {
		moved, err := a.parent.realloc(p, oldSize, size)
		if err != nil {
			return nil, err
		}
		if moved != nil {
			a.big[a.bigIndex(p)] = bigAlloc{p: moved, size: size}
		}
		return moved, nil
	}
	moved, err := a.moveOut(size)
	if err != nil {
		return nil, err
	}
	n := min(oldSize, size)
	copy(unsafe.Slice((*byte)(moved), n), unsafe.Slice((*byte)(p), n))
	return moved, a.dropLocked(p, oldSize)
}

func (a *Arena) moveOut(size int) (unsafe.Pointer, error) {
	if size <= smallMax {
		return a.bump(size, wordAlign, zeroed)
	}
	p, err := a.parent.alloc(size, wordAlign, zeroed)
	if err != nil {
		return nil, err
	}
	a.big = append(a.big, bigAlloc{p: p, size: size})
	return p, nil
}

func (a *Arena) bigIndex(p unsafe.Pointer) int {
	i := slices.IndexFunc(a.big, func(b bigAlloc) bool { return b.p == p })
	if i < 0 {
		panic(fmt.Sprintf("memory: %#x is not a live allocation of this allocator: already freed, from another allocator, or never allocated", uintptr(p)))
	}
	return i
}

func (a *Arena) free(p unsafe.Pointer, size int) error {
	a.g.enter()
	defer a.g.exit()
	return a.dropLocked(p, size)
}

func (a *Arena) dropLocked(p unsafe.Pointer, size int) error {
	if size > smallMax {
		i := a.bigIndex(p)
		a.big = slices.Delete(a.big, i, i+1)
		return a.parent.free(p, size)
	}
	s := spanOf(p)
	a.verify(s, p, size)
	a.stats.Live--
	a.stats.Bytes -= size
	if p == a.last {
		s.carved = uint32(uintptr(p) & spanMask)
		a.last = nil
	}
	return nil
}

// Mark returns the current position, to Release back to later.
func (a *Arena) Mark() Mark {
	a.g.enter()
	defer a.g.exit()
	m := Mark{span: a.cur, big: len(a.big), stats: a.stats, valid: true}
	if a.cur < len(a.spans) {
		m.carved = int(a.spans[a.cur].carved)
	}
	return m
}

// Release frees everything allocated since m was taken, keeping the memory for
// reuse. It panics on the zero Mark and on a Mark it can tell has been released
// past.
func (a *Arena) Release(m Mark) error {
	a.g.enter()
	defer a.g.exit()
	if !m.valid || m.span > a.cur || m.big > len(a.big) {
		panic("memory: Release to a Mark that is no longer valid")
	}
	if m.span < len(a.spans) {
		s := a.spans[m.span]
		carved := uint32(m.carved)
		if m.carved == 0 {
			carved = s.data
		}
		if m.span == a.cur && carved > s.carved {
			panic("memory: Release to a Mark that is no longer valid")
		}
		s.carved = carved
	}
	return a.rewind(m.span, m.big, m.stats)
}

func (a *Arena) rewind(cur, big int, stats Stats) error {
	for i := cur + 1; i < len(a.spans) && i <= a.cur; i++ {
		a.spans[i].carved = a.spans[i].data
	}
	errs := make([]error, 0, len(a.big)-big)
	for _, b := range a.big[big:] {
		errs = append(errs, a.parent.free(b.p, b.size))
	}
	a.big = a.big[:big]
	a.cur, a.last, a.stats = cur, nil, stats
	return errors.Join(errs...)
}

// Reset frees everything allocated from a, keeping its spans for reuse.
func (a *Arena) Reset() error {
	a.g.enter()
	defer a.g.exit()
	if len(a.spans) > 0 {
		a.spans[0].carved = a.spans[0].data
	}
	return a.rewind(0, 0, Stats{})
}

// Free frees everything allocated from a and returns its spans to the parent. a
// remains usable.
func (a *Arena) Free() error {
	a.g.enter()
	defer a.g.exit()
	errs := make([]error, 0, len(a.spans)+1)
	errs = append(errs, a.rewind(0, 0, Stats{}))
	for _, s := range a.spans {
		errs = append(errs, a.parent.giveSpan(s))
	}
	a.spans = nil
	return errors.Join(errs...)
}

// Stats reports the allocations made since the last Reset, counting a freed
// allocation only once freed.
func (a *Arena) Stats() Stats {
	a.g.enter()
	defer a.g.exit()
	return a.stats
}

// Footprint reports the spans a holds from its parent and the large allocations
// it holds through it.
func (a *Arena) Footprint() Footprint {
	a.g.enter()
	defer a.g.exit()
	var f Footprint
	for _, s := range a.spans {
		f.Mapped += spanSize
		f.Committed += int(s.dirty)
	}
	for _, b := range a.big {
		f.Mapped += b.size
		f.Committed += b.size
	}
	return f
}

func (h *heap) lendSpan() (*span, error) {
	s, err := h.takeSpan()
	if err != nil {
		return nil, err
	}
	h.touched -= int(s.dirty)
	return s, nil
}

func (h *heap) reclaimSpan(s *span) error {
	h.touched += int(s.dirty)
	return h.retire(s)
}

func (l *Local) takeSpan() (*span, error) {
	l.g.enter()
	defer l.g.exit()
	return l.h.lendSpan()
}

func (l *Local) giveSpan(s *span) error {
	l.g.enter()
	defer l.g.exit()
	return l.h.reclaimSpan(s)
}

func (s *Shared) takeSpan() (*span, error) {
	sh := s.acquire()
	defer s.put(sh)
	return sh.h.lendSpan()
}

func (s *Shared) giveSpan(sp *span) error {
	sh := s.acquire()
	defer s.put(sh)
	return sh.h.reclaimSpan(sp)
}
