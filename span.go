package memory

import (
	"errors"
	"fmt"
	"math/bits"
	"sync/atomic"
	"unsafe"
)

const (
	spanSize   = 256 << 10
	spanMask   = spanSize - 1
	chunkSpans = 64
	chunkSize  = chunkSpans * spanSize
	smallMax   = 8 << 10
	hotSpans   = 16
	numClasses = 36
	sideOffset = uint32(unsafe.Sizeof(span{}))
	lineSize   = 64
	maxAlign   = 4 << 10
	wordAlign  = 8
)

type fill uint8

const (
	zeroed fill = iota
	uninit
)

type sizeClass struct {
	size, slots, data, inv uint32
}

func newSizeClass(size uint32) sizeClass {
	align := uint32(lineSize)
	if size&(size-1) == 0 {
		align = max(align, min(size, maxAlign))
	}
	slots := (spanSize - sideOffset) / (size + sideEntry)
	data := dataStart(slots, align)
	for data+slots*size > spanSize {
		slots--
		data = dataStart(slots, align)
	}
	return sizeClass{size: size, slots: slots, data: data, inv: uint32((1<<32 + uint64(size) - 1) / uint64(size))}
}

func dataStart(slots, align uint32) uint32 {
	return (sideOffset + slots*sideEntry + align - 1) &^ (align - 1)
}

func buildClasses() [numClasses]sizeClass {
	var out [numClasses]sizeClass
	i := 0
	for size := uint32(8); size <= 64; size += 8 {
		out[i] = newSizeClass(size)
		i++
	}
	for base := uint32(64); base < smallMax; base *= 2 {
		for step := uint32(1); step <= 4; step++ {
			out[i] = newSizeClass(base + base*step/4)
			i++
		}
	}
	return out
}

const (
	fineStep    = 8
	fineLimit   = 1 << 10
	coarseStep  = 128
	fineCount   = fineLimit/fineStep + 1
	coarseCount = (smallMax-fineLimit)/coarseStep + 1
)

func buildFine() [fineCount]uint8 {
	var out [fineCount]uint8
	c := 0
	for i := range out {
		for int(classes[c].size) < i*fineStep {
			c++
		}
		out[i] = uint8(c)
	}
	return out
}

func buildCoarse() [coarseCount]uint8 {
	var out [coarseCount]uint8
	c := 0
	for i := range out {
		for int(classes[c].size) < fineLimit+i*coarseStep {
			c++
		}
		out[i] = uint8(c)
	}
	return out
}

var (
	classes     = buildClasses()
	fineClass   = buildFine()
	coarseClass = buildCoarse()
)

func classFor(size int) uint8 {
	if size <= fineLimit {
		return fineClass[(size+fineStep-1)/fineStep]
	}
	return coarseClass[(size-fineLimit+coarseStep-1)/coarseStep]
}

func classForAligned(size, align int) uint8 {
	if align <= wordAlign {
		return classFor(size)
	}
	return classFor(1 << bits.Len(uint(max(size, align)-1)))
}

type freeSlot struct{ next *freeSlot }

type span struct {
	free         *freeSlot
	next, prev   *span
	slot, inv    uint32
	data, slots  uint32
	carved, used uint32
	dirty        uint32
	owner, shard uint32
	class        uint8
	listed       bool
}

func spanOf(p unsafe.Pointer) *span {
	return (*span)(unsafe.Add(p, -int(uintptr(p)&spanMask)))
}

func (s *span) exhausted() bool { return s.free == nil && s.carved == s.slots }

type heap struct {
	cur      [numClasses]*span
	partial  [numClasses]*span
	hot      []*span
	cold     []*span
	chunks   []unsafe.Pointer
	remote   atomic.Pointer[freeSlot]
	peers    *Shared
	stats    Stats
	touched  int
	deferred error
	owner    uint32
	shard    uint32
}

var owners atomic.Uint32

func nextOwner() uint32 {
	for {
		if id := owners.Add(1); id != 0 {
			return id
		}
	}
}

func (h *heap) alloc(size int, c uint8, f fill) (unsafe.Pointer, error) {
	s := h.cur[c]
	if s == nil || s.exhausted() {
		var err error
		if s, err = h.refill(c); err != nil {
			return nil, err
		}
	}
	var p unsafe.Pointer
	dirty := true
	if slot := s.free; slot != nil {
		s.free = slot.next
		p = unsafe.Pointer(slot)
	} else {
		off := s.data + s.carved*s.slot
		s.carved++
		dirty = off < s.dirty
		if end := off + s.slot; end > s.dirty {
			h.touched += int(end - s.dirty)
			s.dirty = end
		}
		p = unsafe.Add(unsafe.Pointer(s), off)
	}
	s.used++
	h.stats.Live++
	h.stats.Bytes += int(s.slot)
	if dirty && f == zeroed {
		clear(unsafe.Slice((*byte)(p), size))
	}
	stampSlot(s, p, size)
	return p, nil
}

func (h *heap) refill(c uint8) (*span, error) {
	if err := h.drain(); err != nil {
		return nil, err
	}
	if s := h.cur[c]; s != nil && !s.exhausted() {
		return s, nil
	}
	if s := h.partial[c]; s != nil {
		h.unlink(s)
		h.cur[c] = s
		return s, nil
	}
	s, err := h.takeSpan()
	if err != nil {
		return nil, err
	}
	h.format(s, c)
	h.cur[c] = s
	return s, nil
}

func (h *heap) format(s *span, c uint8) {
	k := &classes[c]
	dirty := s.dirty
	if dirty > sideOffset && k.data > sideOffset {
		clear(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(s), sideOffset)), min(dirty, k.data)-sideOffset))
	}
	if k.data > dirty {
		h.touched += int(k.data - dirty)
		dirty = k.data
	}
	*s = span{slot: k.size, inv: k.inv, data: k.data, slots: k.slots, dirty: dirty, owner: h.owner, shard: h.shard, class: c}
}

func (h *heap) takeSpan() (*span, error) {
	if n := len(h.hot); n > 0 {
		s := h.hot[n-1]
		h.hot = h.hot[:n-1]
		return s, nil
	}
	if len(h.cold) == 0 {
		if err := h.mapChunk(); err != nil {
			return nil, err
		}
	}
	n := len(h.cold)
	s := h.cold[n-1]
	if err := osRecommit(unsafe.Pointer(s), spanSize); err != nil {
		return nil, fmt.Errorf("commit span at %#x: %w", uintptr(unsafe.Pointer(s)), err)
	}
	h.cold = h.cold[:n-1]
	return s, nil
}

func (h *heap) mapChunk() error {
	if h.owner == 0 {
		h.owner = nextOwner()
	}
	base, err := osReserve(chunkSize, chunkSize)
	if err != nil {
		return fmt.Errorf("map %d bytes: %w", chunkSize, err)
	}
	h.chunks = append(h.chunks, base)
	for i := chunkSpans - 1; i >= 0; i-- {
		h.cold = append(h.cold, (*span)(unsafe.Add(base, i*spanSize)))
	}
	return nil
}

func (h *heap) link(s *span) {
	s.listed = true
	s.prev = nil
	s.next = h.partial[s.class]
	if s.next != nil {
		s.next.prev = s
	}
	h.partial[s.class] = s
}

func (h *heap) unlink(s *span) {
	if s.prev != nil {
		s.prev.next = s.next
	} else {
		h.partial[s.class] = s.next
	}
	if s.next != nil {
		s.next.prev = s.prev
	}
	s.next, s.prev, s.listed = nil, nil, false
}

func (h *heap) release(s *span, p unsafe.Pointer) error {
	if s.shard != h.shard {
		h.peers.shardAt(s.shard).h.pushRemote(p)
		return nil
	}
	return h.reclaim(s, p)
}

func (h *heap) pushRemote(p unsafe.Pointer) {
	slot := (*freeSlot)(p)
	for {
		head := h.remote.Load()
		slot.next = head
		if h.remote.CompareAndSwap(head, slot) {
			return
		}
	}
}

func (h *heap) drain() error {
	var errs []error
	for slot := h.remote.Swap(nil); slot != nil; {
		next := slot.next
		if err := h.reclaim(spanOf(unsafe.Pointer(slot)), unsafe.Pointer(slot)); err != nil {
			errs = append(errs, err)
		}
		slot = next
	}
	return errors.Join(errs...)
}

func (h *heap) reclaim(s *span, p unsafe.Pointer) error {
	slot := (*freeSlot)(p)
	slot.next = s.free
	s.free = slot
	s.used--
	h.stats.Live--
	h.stats.Bytes -= int(s.slot)
	if h.cur[s.class] == s {
		return nil
	}
	if s.used == 0 {
		if s.listed {
			h.unlink(s)
		}
		return h.retire(s)
	}
	if !s.listed {
		h.link(s)
	}
	return nil
}

func (h *heap) retire(s *span) error {
	if len(h.hot) < hotSpans {
		h.hot = append(h.hot, s)
		return nil
	}
	return h.decommit(s)
}

func (h *heap) decommit(s *span) error {
	dirty := int(s.dirty)
	if err := osDecommit(unsafe.Pointer(s), spanSize); err != nil {
		return fmt.Errorf("decommit span at %#x: %w", uintptr(unsafe.Pointer(s)), err)
	}
	h.touched -= dirty
	h.cold = append(h.cold, s)
	return nil
}

func (h *heap) trim() error {
	errs := []error{h.deferred, h.drain()}
	h.deferred = nil
	for c, s := range h.cur {
		if s != nil && s.used == 0 {
			h.cur[c] = nil
			errs = append(errs, h.decommit(s))
		}
	}
	for _, s := range h.hot {
		errs = append(errs, h.decommit(s))
	}
	h.hot = nil
	errs = append(errs, h.unmapEmptyChunks())
	return errors.Join(errs...)
}

func chunkOf(s *span) uintptr { return uintptr(unsafe.Pointer(s)) &^ (chunkSize - 1) }

func (h *heap) unmapEmptyChunks() error {
	empty := make(map[uintptr]int, len(h.chunks))
	for _, s := range h.cold {
		empty[chunkOf(s)]++
	}
	var errs []error
	removed := map[uintptr]bool{}
	kept := h.chunks[:0]
	for _, base := range h.chunks {
		if empty[uintptr(base)] != chunkSpans {
			kept = append(kept, base)
			continue
		}
		removed[uintptr(base)] = true
		errs = append(errs, unmap(base, chunkSize))
	}
	h.chunks = kept
	cold := h.cold[:0]
	for _, s := range h.cold {
		if !removed[chunkOf(s)] {
			cold = append(cold, s)
		}
	}
	h.cold = cold
	return errors.Join(errs...)
}

func (h *heap) close() error {
	errs := make([]error, 0, len(h.chunks)+1)
	errs = append(errs, h.deferred)
	h.deferred = nil
	for _, base := range h.chunks {
		errs = append(errs, unmap(base, chunkSize))
	}
	h.cur, h.partial = [numClasses]*span{}, [numClasses]*span{}
	h.hot, h.cold, h.chunks = nil, nil, nil
	h.remote.Store(nil)
	h.stats, h.touched, h.owner = Stats{}, 0, 0
	return errors.Join(errs...)
}

func (h *heap) footprint() Footprint {
	retained := 0
	for _, s := range h.hot {
		retained += int(s.dirty)
	}
	return Footprint{Mapped: len(h.chunks) * chunkSize, Committed: h.touched, Retained: retained}
}
