package memory

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sync"
	"unsafe"
)

const (
	hotBytes    = 4 << 20
	hotMapping  = 32 << 20
	liveHotPart = 8
	retainSlack = 4 << 20
)

var osPageSize = os.Getpagesize()

func pageRound(n int) (int, error) {
	if n > math.MaxInt-osPageSize+1 {
		return 0, ErrTooLarge
	}
	return (n + osPageSize - 1) &^ (osPageSize - 1), nil
}

func withTrailer(size int) (int, error) {
	if size > math.MaxInt-2*16 {
		return 0, ErrTooLarge
	}
	return trailerOffset(size) + trailerSize, nil
}

type mapping struct {
	base unsafe.Pointer
	size int
	cold bool
}

type liveMapping struct {
	reserved, size int
}

type direct struct {
	mu         sync.Mutex
	live       map[unsafe.Pointer]liveMapping
	freed      []mapping
	freedBytes int
	hot        int
	liveBytes  int
	peak       int
	stats      Stats
}

func (d *direct) make(size int, f fill) (unsafe.Pointer, error) {
	total, err := withTrailer(size)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.makeLocked(size, total, f)
}

func (d *direct) makeLocked(size, total int, f fill) (unsafe.Pointer, error) {
	base, reserved, err := d.alloc(total, f)
	if err != nil {
		return nil, err
	}
	d.live[base] = liveMapping{reserved: reserved, size: size}
	d.stats.Live++
	d.stats.Bytes += reserved - trailerSize
	stampTrailer(base, size)
	return base, nil
}

func (d *direct) verify(p unsafe.Pointer, size int) liveMapping {
	m, ok := d.live[p]
	if !ok {
		panic(fmt.Sprintf("memory: %#x is not a live allocation of this allocator: already freed, from another allocator, or never allocated", uintptr(p)))
	}
	if m.size != size {
		panic(fmt.Sprintf("memory: %#x was resized since this handle was made, which already freed that handle", uintptr(p)))
	}
	return m
}

func (d *direct) check(p unsafe.Pointer, size int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.verify(p, size)
}

func (d *direct) release(p unsafe.Pointer, size int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.releaseLocked(p, size)
}

func (d *direct) releaseLocked(p unsafe.Pointer, size int) error {
	m := d.verify(p, size)
	eraseTrailer(p, size)
	d.stats.Live--
	d.stats.Bytes -= m.reserved - trailerSize
	return d.free(p)
}

func (d *direct) resize(p unsafe.Pointer, oldSize, size int) (unsafe.Pointer, error) {
	total, err := withTrailer(size)
	if err != nil {
		return nil, err
	}
	rounded, err := pageRound(total)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.verify(p, oldSize)
	if rounded == m.reserved {
		d.resizeInPlace(p, m, oldSize, size)
		return p, nil
	}
	if rounded < m.reserved {
		shrunk, err := osShrink(p, m.reserved, rounded)
		if err != nil {
			return nil, fmt.Errorf("shrink %d bytes at %#x to %d: %w", m.reserved, uintptr(p), rounded, err)
		}
		if shrunk {
			d.account(m.reserved, rounded)
			d.resizeInPlace(p, liveMapping{reserved: rounded, size: oldSize}, oldSize, size)
			return p, nil
		}
	}
	moved, err := d.makeLocked(size, total, zeroed)
	if err != nil {
		return nil, err
	}
	n := min(oldSize, size)
	copy(unsafe.Slice((*byte)(moved), n), unsafe.Slice((*byte)(p), n))
	return moved, d.releaseLocked(p, oldSize)
}

func (d *direct) account(oldReserved, reserved int) {
	d.liveBytes += reserved - oldReserved
	d.stats.Bytes += reserved - oldReserved
}

func (d *direct) resizeInPlace(p unsafe.Pointer, m liveMapping, oldSize, size int) {
	if Checked && trailerOffset(oldSize)+trailerSize <= m.reserved {
		supersedeTrailer(p, oldSize)
	}
	clearFrom(p, oldSize, size)
	stampTrailer(p, size)
	d.live[p] = liveMapping{reserved: m.reserved, size: size}
}

func clearFrom(p unsafe.Pointer, from, to int) {
	if to > from {
		clear(unsafe.Slice((*byte)(unsafe.Add(p, from)), to-from))
	}
}

func (d *direct) alloc(total int, f fill) (unsafe.Pointer, int, error) {
	size, err := pageRound(total)
	if err != nil {
		return nil, 0, err
	}
	if i := d.reusable(size); i >= 0 {
		return d.reuse(i, total, f)
	}
	base, err := osMap(size)
	if err != nil {
		return nil, 0, fmt.Errorf("map %d bytes: %w", size, err)
	}
	d.admit(base, size)
	return base, size, nil
}

func (d *direct) reusable(size int) int {
	for i, m := range slices.Backward(d.freed) {
		if m.size == size {
			return i
		}
	}
	return -1
}

func (d *direct) reuse(i, total int, f fill) (unsafe.Pointer, int, error) {
	m := d.freed[i]
	dirtyFrom := 0
	if m.cold {
		dirtyFrom = m.size - osPageSize
		if err := osRecommit(m.base, dirtyFrom); err != nil {
			return nil, 0, fmt.Errorf("recommit %d bytes at %#x: %w", m.size, uintptr(m.base), err)
		}
	} else {
		d.hot -= m.size
	}
	d.freed = slices.Delete(d.freed, i, i+1)
	d.freedBytes -= m.size
	if f == zeroed {
		clearFrom(m.base, dirtyFrom, total)
	}
	d.admit(m.base, m.size)
	return m.base, m.size, nil
}

func (d *direct) admit(base unsafe.Pointer, size int) {
	if d.live == nil {
		d.live = map[unsafe.Pointer]liveMapping{}
	}
	d.live[base] = liveMapping{reserved: size}
	d.liveBytes += size
	d.peak = max(d.peak, d.liveBytes)
}

func (d *direct) reserved(base unsafe.Pointer) (int, bool) {
	m, ok := d.live[base]
	return m.reserved, ok
}

func (d *direct) free(base unsafe.Pointer) error {
	size := d.live[base].reserved
	delete(d.live, base)
	d.liveBytes -= size
	d.freed = append(d.freed, mapping{base: base, size: size})
	d.freedBytes += size
	d.hot += size
	return errors.Join(d.cool(size), d.evict())
}

func (d *direct) hotBudget(newest int) int {
	return max(hotBytes, d.liveBytes/liveHotPart, min(newest, hotMapping))
}

func (d *direct) cool(newest int) error {
	budget := d.hotBudget(newest)
	for i := range d.freed {
		if d.hot <= budget {
			return nil
		}
		m := &d.freed[i]
		if m.cold {
			continue
		}
		if m.size > osPageSize {
			if err := osDecommit(m.base, m.size-osPageSize); err != nil {
				return fmt.Errorf("decommit %d bytes at %#x: %w", m.size, uintptr(m.base), err)
			}
		}
		m.cold = true
		d.hot -= m.size
	}
	return nil
}

func (d *direct) evict() error {
	n := 0
	for n < len(d.freed)-1 && d.freedBytes > d.peak+retainSlack {
		d.freedBytes -= d.freed[n].size
		if !d.freed[n].cold {
			d.hot -= d.freed[n].size
		}
		n++
	}
	errs := make([]error, 0, n)
	for _, m := range d.freed[:n] {
		errs = append(errs, unmap(m.base, m.size))
	}
	d.freed = slices.Delete(d.freed, 0, n)
	return errors.Join(errs...)
}

func unmap(base unsafe.Pointer, size int) error {
	if err := osUnmap(base, size); err != nil {
		return fmt.Errorf("unmap %d bytes at %#x: %w", size, uintptr(base), err)
	}
	return nil
}

func (d *direct) trim() error {
	errs := make([]error, 0, len(d.freed))
	for _, m := range d.freed {
		errs = append(errs, unmap(m.base, m.size))
	}
	d.freed, d.freedBytes, d.hot = nil, 0, 0
	return errors.Join(errs...)
}

func (d *direct) close() error {
	live := make([]mapping, 0, len(d.live))
	for base, m := range d.live {
		live = append(live, mapping{base: base, size: m.reserved})
	}
	slices.SortFunc(live, func(a, b mapping) int { return cmp.Compare(uintptr(a.base), uintptr(b.base)) })
	errs := make([]error, 0, len(live)+1)
	for _, m := range live {
		errs = append(errs, unmap(m.base, m.size))
	}
	errs = append(errs, d.trim())
	d.live, d.liveBytes, d.peak, d.stats = nil, 0, 0, Stats{}
	return errors.Join(errs...)
}

func (d *direct) snapshot() (Stats, Footprint) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stats, Footprint{
		Mapped:    d.liveBytes + d.freedBytes,
		Committed: d.liveBytes + d.hot,
		Retained:  d.freedBytes,
	}
}

func (d *direct) trimLocked() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.trim()
}

func (d *direct) closeLocked() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.close()
}
