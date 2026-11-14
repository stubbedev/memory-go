package heapslab_test

import (
	"errors"
	"strconv"
	"testing"
	"unsafe"

	"github.com/stubbedev/memory-go/heapslab"
	"github.com/stubbedev/memory-go/internal/testkit"
)

type node struct {
	key         int
	name        string
	left, right *node
}

func TestRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	if _, err := heapslab.NewPool[node](0); !errors.Is(err, heapslab.ErrChunkSize) {
		t.Errorf("pool chunk 0: err = %v, want ErrChunkSize", err)
	}
	if _, err := heapslab.NewArena[node](-1); !errors.Is(err, heapslab.ErrChunkSize) {
		t.Errorf("arena chunk -1: err = %v, want ErrChunkSize", err)
	}
	if _, err := heapslab.NewPool[struct{}](4); !errors.Is(err, heapslab.ErrZeroSize) {
		t.Errorf("zero-size pool: err = %v, want ErrZeroSize", err)
	}
	a, err := heapslab.NewArena[node](4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("AllocSlice(-1) did not panic")
		}
	}()
	a.AllocSlice(-1)
}

func TestPoolRecyclesZeroed(t *testing.T) {
	t.Parallel()

	p, err := heapslab.NewPool[node](4)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	items := make([]heapslab.Item[node], 10)
	for i := range items {
		items[i] = p.Get()
		*items[i].Value() = node{key: i, name: strconv.Itoa(i), left: &node{}}
	}
	if p.Len() != 10 {
		t.Fatalf("Len = %d, want 10", p.Len())
	}
	seen := map[*node]bool{}
	for i, it := range items {
		v := it.Value()
		if v.key != i || v.name != strconv.Itoa(i) || seen[v] {
			t.Fatalf("item %d = %+v, overwritten or shared", i, *v)
		}
		seen[v] = true
		p.Put(it)
	}
	for range items {
		it := p.Get()
		v := it.Value()
		if *v != (node{}) {
			t.Fatalf("recycled value %+v, want zero", *v)
		}
		if !seen[v] {
			t.Fatal("Get did not reuse a put-back slot")
		}
	}
	p.Reset()
	if p.Len() != 0 {
		t.Fatalf("Len after Reset = %d", p.Len())
	}
	if v := p.Get().Value(); *v != (node{}) || !seen[v] {
		t.Fatalf("Get after Reset = %+v, want a zeroed old slot", *v)
	}
}

func TestPutZeroItemPanics(t *testing.T) {
	t.Parallel()

	p, err := heapslab.NewPool[node](4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if msg, _ := recover().(string); msg != "heapslab: Put of the zero Item" {
			t.Fatalf("panic = %q, want the zero-Item panic", msg)
		}
	}()
	if !(heapslab.Item[node]{}).IsNil() {
		t.Fatal("zero Item is not nil")
	}
	p.Put(heapslab.Item[node]{})
}

func TestArenaAllocationsAreDistinctAndStable(t *testing.T) {
	t.Parallel()

	a, err := heapslab.NewArena[node](16)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Free()
	var head *node
	for i := range 1000 {
		n := a.Alloc()
		if *n != (node{}) {
			t.Fatalf("Alloc %d not zeroed: %+v", i, *n)
		}
		n.key, n.left = i, head
		head = n
	}
	big, small := a.AllocSlice(100), a.AllocSlice(10)
	if len(big) != 100 || cap(big) != 100 || len(small) != 10 || cap(small) != 10 {
		t.Fatalf("len/cap = %d/%d, %d/%d", len(big), cap(big), len(small), cap(small))
	}
	for i := range big {
		big[i].key = -1
	}
	for i := range small {
		small[i].key = -2
	}
	want := 999
	for n := head; n != nil; n = n.left {
		if n.key != want {
			t.Fatalf("node %d has key %d, overwritten", want, n.key)
		}
		want--
	}
	if want != -1 {
		t.Fatalf("walked to %d, want -1", want)
	}
}

func TestArenaResetReusesZeroed(t *testing.T) {
	t.Parallel()

	a, err := heapslab.NewArena[node](8)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Free()
	first := map[*node]bool{}
	for range 20 {
		n := a.Alloc()
		n.name = "x"
		first[n] = true
	}
	big := a.AllocSlice(50)
	big[0].name = "big"
	a.Reset()
	if big[0].name != "" {
		t.Fatal("Reset did not zero an oversized slice")
	}
	for i := range 20 {
		n := a.Alloc()
		if *n != (node{}) {
			t.Fatalf("Alloc %d after Reset = %+v, want zero", i, *n)
		}
		if !first[n] {
			t.Fatalf("Alloc %d after Reset did not reuse a chunk", i)
		}
	}
	a.Free()
	a.Free()
}

func TestResetDoesNotAllocate(t *testing.T) {
	a, err := heapslab.NewArena[node](64)
	if err != nil {
		t.Fatal(err)
	}
	p, err := heapslab.NewPool[node](64)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]heapslab.Item[node], 200)
	cycle := func() {
		for range 200 {
			a.Alloc()
		}
		a.Reset()
		for i := range items {
			items[i] = p.Get()
		}
		for _, it := range items {
			p.Put(it)
		}
		p.Reset()
	}
	cycle()
	if n := testing.AllocsPerRun(10, cycle); n != 0 {
		t.Fatalf("warm cycle allocated %v times, want 0", n)
	}
}

func FuzzPoolMatchesModel(f *testing.F) {
	f.Add(testkit.Seed(9, 1<<15))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := heapslab.NewPool[node](1 + len(data)%17)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Free()
		ops := testkit.NewOps(data)
		var items []heapslab.Item[node]
		var keys []int
		for step := 0; ops.More(); step++ {
			switch op := ops.Int(16); {
			case op == 0:
				p.Reset()
				items, keys = items[:0], keys[:0]
			case op < 10 || len(items) == 0:
				it := p.Get()
				if *it.Value() != (node{}) {
					t.Fatalf("step %d: Get returned %+v, want zero", step, *it.Value())
				}
				key := int(ops.Uint64() >> 1)
				*it.Value() = node{key: key, name: strconv.Itoa(key)}
				if len(items) > 0 {
					it.Value().left = items[len(items)-1].Value()
				}
				items, keys = append(items, it), append(keys, key)
			default:
				i := ops.Int(len(items))
				p.Put(items[i])
				items[i], keys[i] = items[len(items)-1], keys[len(keys)-1]
				items, keys = items[:len(items)-1], keys[:len(keys)-1]
			}
			if p.Len() != len(items) {
				t.Fatalf("step %d: Len = %d, want %d", step, p.Len(), len(items))
			}
		}
		for i, it := range items {
			if v := it.Value(); v.key != keys[i] || v.name != strconv.Itoa(keys[i]) {
				t.Fatalf("item %d = %+v, want key %d", i, *v, keys[i])
			}
		}
	})
}

func FuzzArenaMatchesModel(f *testing.F) {
	f.Add(testkit.Seed(10, 1<<15))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		chunk := 1 + len(data)%17
		a, err := heapslab.NewArena[node](chunk)
		if err != nil {
			t.Fatal(err)
		}
		defer a.Free()
		ops := testkit.NewOps(data)
		var runs [][]node
		var keys []int
		for step := 0; ops.More(); step++ {
			switch op := ops.Int(16); {
			case op == 0:
				a.Reset()
				runs, keys = runs[:0], keys[:0]
			case op < 8:
				n := a.Alloc()
				if *n != (node{}) {
					t.Fatalf("step %d: Alloc returned %+v, want zero", step, *n)
				}
				key := len(keys)
				n.key, n.name = key, strconv.Itoa(key)
				runs, keys = append(runs, unsafe.Slice(n, 1)), append(keys, key)
			default:
				s := a.AllocSlice(ops.Int(2 * chunk))
				if cap(s) != len(s) {
					t.Fatalf("step %d: cap %d, len %d", step, cap(s), len(s))
				}
				key := len(keys)
				for i := range s {
					if s[i] != (node{}) {
						t.Fatalf("step %d: AllocSlice element %d = %+v, want zero", step, i, s[i])
					}
					s[i].key, s[i].name = key, strconv.Itoa(key)
				}
				runs, keys = append(runs, s), append(keys, key)
			}
		}
		for r, run := range runs {
			for i := range run {
				if run[i].key != keys[r] || run[i].name != strconv.Itoa(keys[r]) {
					t.Fatalf("run %d element %d = %+v, want key %d", r, i, run[i], keys[r])
				}
			}
		}
	})
}
