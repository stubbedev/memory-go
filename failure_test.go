package memory_test

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/btree"
	"github.com/stubbedev/memory-go/bufpool"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/intern"
	"github.com/stubbedev/memory-go/linkedlist"
	"github.com/stubbedev/memory-go/minheap"
	"github.com/stubbedev/memory-go/pool"
	"github.com/stubbedev/memory-go/typedarena"
	"github.com/stubbedev/memory-go/vector"
)

const sweepSteps = 300

type subject struct {
	step   func(i int) error
	verify func(n int) error
	free   func() error
}

type subjectFactory func(a memory.Allocator) (subject, error)

func expect(got, want any, what string) error {
	if got != want {
		return fmt.Errorf("%s = %v, want %v", what, got, want)
	}
	return nil
}

func vectorSubject(a memory.Allocator) (subject, error) {
	v, err := vector.New[int](a, 0)
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error { return v.Push(i) },
		verify: func(n int) error {
			if err := expect(v.Len(), n, "Len"); err != nil {
				return err
			}
			for i := range n {
				if err := expect(v.At(i), i, "At"); err != nil {
					return err
				}
			}
			return nil
		},
		free: v.Free,
	}, nil
}

func mapSubject(a memory.Allocator) (subject, error) {
	m, err := hashmap.New[int, int](a, 0)
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error { return m.Set(i, i) },
		verify: func(n int) error {
			if err := expect(m.Len(), n, "Len"); err != nil {
				return err
			}
			for i := range n {
				if got, ok := m.Get(i); !ok || got != i {
					return fmt.Errorf("Get(%d) = %d, %v", i, got, ok)
				}
			}
			return nil
		},
		free: m.Free,
	}, nil
}

func stringsSubject(a memory.Allocator) (subject, error) {
	m, err := hashmap.NewStrings[int](a, 0)
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error { return m.Set(strconv.Itoa(i), i) },
		verify: func(n int) error {
			if err := expect(m.Len(), n, "Len"); err != nil {
				return err
			}
			for i := range n {
				if got, ok := m.Get(strconv.Itoa(i)); !ok || got != i {
					return fmt.Errorf("Get(%d) = %d, %v", i, got, ok)
				}
			}
			return nil
		},
		free: m.Free,
	}, nil
}

func btreeSubject(a memory.Allocator) (subject, error) {
	tree, err := btree.New[int, int](a, cmp.Compare[int])
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error { return tree.Set(i, i) },
		verify: func(n int) error {
			if err := expect(tree.Len(), n, "Len"); err != nil {
				return err
			}
			for i := range n {
				if got, ok := tree.Get(i); !ok || got != i {
					return fmt.Errorf("Get(%d) = %d, %v", i, got, ok)
				}
			}
			return nil
		},
		free: tree.Free,
	}, nil
}

func internSubject(a memory.Allocator) (subject, error) {
	table, err := intern.New(a)
	if err != nil {
		return subject{}, err
	}
	symbols := map[int]intern.Symbol{}
	return subject{
		step: func(i int) error {
			sym, err := table.Intern(strconv.Itoa(i))
			if err == nil {
				symbols[i] = sym
			}
			return err
		},
		verify: func(n int) error {
			if err := expect(table.Len(), n, "Len"); err != nil {
				return err
			}
			for i := range n {
				if err := expect(table.Text(symbols[i]), strconv.Itoa(i), "Text"); err != nil {
					return err
				}
				if sym, ok := table.Lookup(strconv.Itoa(i)); !ok || sym != symbols[i] {
					return fmt.Errorf("Lookup(%d) = %d, %v", i, sym, ok)
				}
			}
			return nil
		},
		free: table.Free,
	}, nil
}

func listSubject(a memory.Allocator) (subject, error) {
	l, err := linkedlist.New[int](a)
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error { return l.PushBack(i) },
		verify: func(n int) error {
			if err := expect(l.Len(), n, "Len"); err != nil {
				return err
			}
			for i, got := range l.All() {
				if err := expect(got, i, "element"); err != nil {
					return err
				}
			}
			return nil
		},
		free: l.Free,
	}, nil
}

func heapSubject(a memory.Allocator) (subject, error) {
	h, err := minheap.New[int](a, func(x, y int) bool { return x < y })
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error { return h.Push(i) },
		verify: func(n int) error {
			if err := expect(h.Len(), n, "Len"); err != nil {
				return err
			}
			if n == 0 {
				return nil
			}
			got, ok := h.Peek()
			if !ok || got != 0 {
				return fmt.Errorf("Peek = %d, %v", got, ok)
			}
			return nil
		},
		free: h.Free,
	}, nil
}

func poolSubject(a memory.Allocator) (subject, error) {
	p, err := pool.New[int](a, 16)
	if err != nil {
		return subject{}, err
	}
	var items []pool.Item[int]
	return subject{
		step: func(i int) error {
			it, err := p.Get()
			if err != nil {
				return err
			}
			*it.Value() = i
			items = append(items, it)
			return nil
		},
		verify: func(n int) error {
			if err := expect(len(items), n, "items"); err != nil {
				return err
			}
			for i, it := range items {
				if err := expect(*it.Value(), i, "item"); err != nil {
					return err
				}
			}
			return nil
		},
		free: p.Free,
	}, nil
}

func typedArenaSubject(a memory.Allocator) (subject, error) {
	ta, err := typedarena.New[int](a, 16)
	if err != nil {
		return subject{}, err
	}
	var refs []memory.Ref[int]
	return subject{
		step: func(i int) error {
			r, err := ta.Alloc()
			if err != nil {
				return err
			}
			*r.Value() = i
			refs = append(refs, r)
			return nil
		},
		verify: func(n int) error {
			if err := expect(len(refs), n, "refs"); err != nil {
				return err
			}
			for i, r := range refs {
				if err := expect(*r.Value(), i, "ref"); err != nil {
					return err
				}
			}
			return nil
		},
		free: ta.Free,
	}, nil
}

func bufpoolSubject(a memory.Allocator) (subject, error) {
	p, err := bufpool.New(a, 1<<16)
	if err != nil {
		return subject{}, err
	}
	return subject{
		step: func(i int) error {
			b, err := p.Get(1 << (12 + i%4))
			if err != nil {
				return err
			}
			return p.Put(b)
		},
		verify: func(int) error { return nil },
		free:   p.Trim,
	}, nil
}

var subjects = map[string]subjectFactory{
	"vector":     vectorSubject,
	"map":        mapSubject,
	"strings":    stringsSubject,
	"btree":      btreeSubject,
	"intern":     internSubject,
	"linkedlist": listSubject,
	"minheap":    heapSubject,
	"pool":       poolSubject,
	"typedarena": typedArenaSubject,
	"bufpool":    bufpoolSubject,
}

func runWithFailure(t *testing.T, build subjectFactory, arm func(*memory.Faulty, int), k int) bool {
	t.Helper()
	var l memory.Local
	defer l.Close()
	f := memory.NewFaulty(&l)
	disarm := func() {
		f.FailAllocsAfter(-1)
		f.FailFreesAfter(-1)
	}
	arm(f, k)
	fired := false
	s, err := build(f)
	for err != nil {
		if !errors.Is(err, memory.ErrInjected) {
			t.Fatalf("k=%d: constructor: %v", k, err)
		}
		fired = true
		disarm()
		if live := l.Stats().Live; live != 0 {
			t.Fatalf("k=%d: failed constructor left %d allocations", k, live)
		}
		s, err = build(f)
	}
	for i := range sweepSteps {
		if err := s.step(i); err != nil {
			if !errors.Is(err, memory.ErrInjected) {
				t.Fatalf("k=%d: step %d: %v", k, i, err)
			}
			fired = true
			disarm()
			if err := s.verify(i); err != nil {
				t.Fatalf("k=%d: after failed step %d: %v", k, i, err)
			}
			if err := s.step(i); err != nil {
				t.Fatalf("k=%d: retrying step %d: %v", k, i, err)
			}
		}
	}
	if err := s.verify(sweepSteps); err != nil {
		t.Fatalf("k=%d: %v", k, err)
	}
	if err := s.free(); err != nil {
		if !errors.Is(err, memory.ErrInjected) {
			t.Fatalf("k=%d: free: %v", k, err)
		}
		fired = true
	}
	return fired
}

func TestContainersSurviveAllocationFailure(t *testing.T) {
	t.Parallel()

	for name, build := range subjects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for k := 0; runWithFailure(t, build, (*memory.Faulty).FailAllocsAfter, k); k++ {
				if k > 10*sweepSteps {
					t.Fatal("failure kept firing")
				}
			}
		})
	}
}

func TestContainersSurviveFreeFailure(t *testing.T) {
	t.Parallel()

	for name, build := range subjects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for k := 0; runWithFailure(t, build, (*memory.Faulty).FailFreesAfter, k); k++ {
				if k > 10*sweepSteps {
					t.Fatal("failure kept firing")
				}
			}
		})
	}
}

func TestFaultyFailsAndRecovers(t *testing.T) {
	t.Parallel()

	var l memory.Local
	defer l.Close()
	f := memory.NewFaulty(&l)
	f.FailAllocsAfter(1)
	s, err := memory.MakeSlice[byte](f, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.MakeSlice[byte](f, 16); !errors.Is(err, memory.ErrInjected) {
		t.Fatalf("second allocation: %v", err)
	}
	if got, err := memory.ResizeSlice(f, s, 1<<20); !errors.Is(err, memory.ErrInjected) || got != s {
		t.Fatalf("failed resize = %v, %v, want the input back", got.Len(), err)
	}
	f.FailAllocsAfter(-1)
	f.FailFreesAfter(0)
	grown, err := memory.ResizeSlice(f, s, 1<<20)
	if !errors.Is(err, memory.ErrInjected) || grown.Len() != 1<<20 {
		t.Fatalf("resize with failing free = %d, %v", grown.Len(), err)
	}
	if err := memory.FreeSlice(f, grown); !errors.Is(err, memory.ErrInjected) {
		t.Fatalf("free: %v", err)
	}
	if got := f.Stats(); got != (memory.Stats{}) {
		t.Fatalf("Stats = %+v", got)
	}
}
