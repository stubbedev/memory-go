package memory_test

import (
	"slices"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/testkit"
)

var fuzzSizes = []int{1, 7, 8, 9, 24, 64, 65, 100, 1000, 1024, 1025, 4096, 8191, 8192, 8193, 8208, 12288, 1 << 16, 1 << 20, 3 << 20}

const fuzzInput = 2048

type tracked struct {
	s    memory.Slice[byte]
	seed byte
	born int
}

func stride(n int) int { return 1 + n/512 }

func paint(s memory.Slice[byte], seed byte) {
	e := s.Elems()
	for i := 0; i < len(e); i += stride(len(e)) {
		e[i] = seed + byte(i)
	}
	e[len(e)-1] = seed ^ 0x5a
}

func verifyPaint(t *testing.T, x tracked) {
	t.Helper()
	e := x.s.Elems()
	for i := 0; i < len(e)-1; i += stride(len(e)) {
		if e[i] != x.seed+byte(i) {
			t.Fatalf("byte %d of %d = %d, want %d", i, len(e), e[i], x.seed+byte(i))
		}
	}
	if e[len(e)-1] != x.seed^0x5a {
		t.Fatalf("last byte of %d = %d", len(e), e[len(e)-1])
	}
}

func checkZero(t *testing.T, e []byte, from int) {
	t.Helper()
	for i := from; i < len(e); i += 1 + (len(e)-from)/4096 {
		if e[i] != 0 {
			t.Fatalf("byte %d of %d not zero", i, len(e))
		}
	}
	if len(e) > from && e[len(e)-1] != 0 {
		t.Fatalf("last byte of %d not zero", len(e))
	}
}

type allocModel struct {
	t    *testing.T
	a    memory.Allocator
	live []tracked
	next int
}

func (m *allocModel) born() int {
	m.next++
	return m.next
}

func (m *allocModel) add(ops *testkit.Ops) {
	n := fuzzSizes[ops.Int(len(fuzzSizes))] + ops.Int(3)
	s, err := memory.MakeSlice[byte](m.a, n)
	if err != nil {
		m.t.Fatal(err)
	}
	checkZero(m.t, s.Elems(), 0)
	x := tracked{s: s, seed: byte(ops.Int(256)), born: m.born()}
	paint(s, x.seed)
	m.live = append(m.live, x)
}

func (m *allocModel) drop(i int) {
	verifyPaint(m.t, m.live[i])
	if err := memory.FreeSlice(m.a, m.live[i].s); err != nil {
		m.t.Fatal(err)
	}
	m.live = slices.Delete(m.live, i, i+1)
}

func (m *allocModel) resize(i int, ops *testkit.Ops) {
	old := m.live[i]
	verifyPaint(m.t, old)
	n := fuzzSizes[ops.Int(len(fuzzSizes))] + ops.Int(5)
	prefix := slices.Clone(old.s.Elems()[:min(old.s.Len(), n)])
	s, err := memory.ResizeSlice(m.a, old.s, n)
	if err != nil {
		m.t.Fatal(err)
	}
	if !slices.Equal(s.Elems()[:len(prefix)], prefix) {
		m.t.Fatalf("resize %d to %d changed the prefix", old.s.Len(), n)
	}
	checkZero(m.t, s.Elems(), len(prefix))
	x := tracked{s: s, seed: byte(ops.Int(256)), born: m.born()}
	paint(s, x.seed)
	m.live[i] = x
}

func (m *allocModel) step(ops *testkit.Ops) {
	op := ops.Int(6)
	switch {
	case op <= 2 || len(m.live) == 0:
		m.add(ops)
	case op == 3:
		m.drop(ops.Int(len(m.live)))
	default:
		m.resize(ops.Int(len(m.live)), ops)
	}
}

func (m *allocModel) verifyAll() {
	for _, x := range m.live {
		verifyPaint(m.t, x)
	}
}

func (m *allocModel) dropAll() {
	for len(m.live) > 0 {
		m.drop(len(m.live) - 1)
	}
}

func FuzzLocal(f *testing.F) {
	f.Add(testkit.Seed(1, fuzzInput))
	f.Fuzz(func(t *testing.T, data []byte) {
		var a memory.Local
		defer a.Close()
		m := &allocModel{t: t, a: &a}
		for ops := testkit.NewOps(data); ops.More(); {
			m.step(ops)
		}
		m.dropAll()
		if s := a.Stats(); s != (memory.Stats{}) {
			t.Fatalf("Stats after freeing everything = %+v", s)
		}
		if err := a.Trim(); err != nil {
			t.Fatal(err)
		}
		if foot := a.Footprint(); foot != (memory.Footprint{}) {
			t.Fatalf("Footprint after Trim = %+v", foot)
		}
	})
}

func FuzzShared(f *testing.F) {
	f.Add(testkit.Seed(2, fuzzInput))
	f.Fuzz(func(t *testing.T, data []byte) {
		var a memory.Shared
		defer a.Close()
		m := &allocModel{t: t, a: &a}
		for ops := testkit.NewOps(data); ops.More(); {
			m.step(ops)
		}
		m.dropAll()
		if s := a.Stats(); s != (memory.Stats{}) {
			t.Fatalf("Stats after freeing everything = %+v", s)
		}
	})
}

type savedMark struct {
	mark memory.Mark
	born int
}

func FuzzArena(f *testing.F) {
	f.Add(testkit.Seed(3, fuzzInput))
	f.Fuzz(func(t *testing.T, data []byte) {
		var parent memory.Local
		defer parent.Close()
		a := memory.NewArena(&parent)
		m := &allocModel{t: t, a: a}
		var marks []savedMark
		for ops := testkit.NewOps(data); ops.More(); {
			switch op := ops.Int(10); {
			case op == 0:
				marks = append(marks, savedMark{mark: a.Mark(), born: m.born()})
			case op == 1 && len(marks) > 0:
				i := ops.Int(len(marks))
				if err := a.Release(marks[i].mark); err != nil {
					t.Fatal(err)
				}
				m.live = slices.DeleteFunc(m.live, func(x tracked) bool { return x.born > marks[i].born })
				marks = marks[:i+1]
				m.verifyAll()
			case op == 2:
				if err := a.Reset(); err != nil {
					t.Fatal(err)
				}
				m.live, marks = nil, nil
			default:
				m.step(ops)
			}
		}
		m.verifyAll()
		if err := a.Free(); err != nil {
			t.Fatal(err)
		}
		if s := parent.Stats(); s != (memory.Stats{}) {
			t.Fatalf("parent Stats after Free = %+v", s)
		}
	})
}
