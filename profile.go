package memory

import (
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"unsafe"
)

type siteSet struct {
	mu   sync.Mutex
	prof *pprof.Profile
	live map[uintptr]struct{}
}

type sites struct {
	set atomic.Pointer[siteSet]
}

const profileSkip = 3

func (s *sites) use(p *pprof.Profile) {
	s.clear()
	if p == nil {
		s.set.Store(nil)
		return
	}
	s.set.Store(&siteSet{prof: p, live: map[uintptr]struct{}{}})
}

func (s *sites) added(p unsafe.Pointer) {
	set := s.set.Load()
	if set == nil || p == nil {
		return
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	if _, dup := set.live[uintptr(p)]; dup {
		return
	}
	set.live[uintptr(p)] = struct{}{}
	set.prof.Add(uintptr(p), profileSkip)
}

func (s *sites) removed(p unsafe.Pointer) {
	set := s.set.Load()
	if set == nil {
		return
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	delete(set.live, uintptr(p))
	set.prof.Remove(uintptr(p))
}

func (s *sites) moved(from, to unsafe.Pointer) {
	if from != to {
		s.removed(from)
		s.added(to)
	}
}

func (s *sites) clear() {
	set := s.set.Load()
	if set == nil {
		return
	}
	set.mu.Lock()
	defer set.mu.Unlock()
	for p := range set.live {
		set.prof.Remove(p)
	}
	clear(set.live)
}

// SetProfile records the call stack of every allocation in p, created with
// pprof.NewProfile, until it is freed, so `go tool pprof` shows where live
// off-heap memory came from. It costs a lock and a stack capture per allocation
// and is meant for finding leaks, not for production. Nil stops recording.
func (l *Local) SetProfile(p *pprof.Profile) { l.prof.use(p) }

// SetProfile behaves like Local.SetProfile.
func (s *Shared) SetProfile(p *pprof.Profile) { s.prof.use(p) }
