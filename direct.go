package memory

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"unsafe"
)

const (
	hotBytes    = 4 << 20
	retainSlack = 4 << 20
)

var (
	osPageSize = os.Getpagesize()
	classMax   = max(8<<10, osPageSize)
)

func isDirect(total int) bool { return total > classMax }

func pageRound(n int) (int, error) {
	if n > math.MaxInt-osPageSize+1 {
		return 0, ErrTooLarge
	}
	return (n + osPageSize - 1) &^ (osPageSize - 1), nil
}

type mapping struct {
	base unsafe.Pointer
	size int
	cold bool
}

type direct struct {
	live       map[unsafe.Pointer]int
	freed      []mapping
	freedBytes int
	liveBytes  int
	peak       int
}

func (d *direct) alloc(total int) (unsafe.Pointer, int, error) {
	size, err := pageRound(total)
	if err != nil {
		return nil, 0, err
	}
	if i := d.reusable(size); i >= 0 {
		return d.reuse(i, total)
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

func (d *direct) reuse(i, total int) (unsafe.Pointer, int, error) {
	m := d.freed[i]
	if m.cold {
		if err := osRecommit(unsafe.Add(m.base, osPageSize), m.size-osPageSize); err != nil {
			return nil, 0, fmt.Errorf("recommit %d bytes at %#x: %w", m.size, uintptr(m.base), err)
		}
	}
	d.freed = slices.Delete(d.freed, i, i+1)
	d.freedBytes -= m.size
	clear(unsafe.Slice((*byte)(m.base), total))
	d.admit(m.base, m.size)
	return m.base, m.size, nil
}

func (d *direct) admit(base unsafe.Pointer, size int) {
	if d.live == nil {
		d.live = map[unsafe.Pointer]int{}
	}
	d.live[base] = size
	d.liveBytes += size
	d.peak = max(d.peak, d.liveBytes)
}

func (d *direct) reserved(base unsafe.Pointer) (int, bool) {
	size, ok := d.live[base]
	return size, ok
}

func (d *direct) free(base unsafe.Pointer) error {
	size := d.live[base]
	delete(d.live, base)
	d.liveBytes -= size
	d.freed = append(d.freed, mapping{base: base, size: size})
	d.freedBytes += size
	return errors.Join(d.cool(size), d.evict())
}

func (d *direct) cool(size int) error {
	hot := max(1, hotBytes/size)
	for i := len(d.freed) - 1; i >= 0; i-- {
		m := &d.freed[i]
		if m.size != size || m.cold {
			continue
		}
		if hot > 0 {
			hot--
			continue
		}
		if size > osPageSize {
			if err := osDecommit(unsafe.Add(m.base, osPageSize), size-osPageSize); err != nil {
				return fmt.Errorf("decommit %d bytes at %#x: %w", size, uintptr(m.base), err)
			}
		}
		m.cold = true
		return nil
	}
	return nil
}

func (d *direct) evict() error {
	n := 0
	for n < len(d.freed)-1 && d.freedBytes > d.peak+retainSlack {
		d.freedBytes -= d.freed[n].size
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
	d.freed, d.freedBytes = nil, 0
	return errors.Join(errs...)
}

func (d *direct) close() error {
	live := make([]mapping, 0, len(d.live))
	for base, size := range d.live {
		live = append(live, mapping{base: base, size: size})
	}
	slices.SortFunc(live, func(a, b mapping) int { return cmp.Compare(uintptr(a.base), uintptr(b.base)) })
	errs := make([]error, 0, len(live)+1)
	for _, m := range live {
		errs = append(errs, unmap(m.base, m.size))
	}
	errs = append(errs, d.trim())
	*d = direct{}
	return errors.Join(errs...)
}
