package btree_test

import (
	"cmp"
	"errors"
	"slices"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/btree"
	"github.com/stubbedev/memory-go/internal/testkit"
	"github.com/stubbedev/memory-go/memtest"
)

func newTree(t testing.TB, a memory.Allocator) *btree.Tree[uint64, uint64] {
	t.Helper()
	tr, err := btree.New[uint64, uint64](a, cmp.Compare[uint64])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tr.Free(); err != nil {
			t.Error(err)
		}
	})
	return tr
}

func TestRejectsPointerTypes(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := btree.New[string, int](a, cmp.Compare[string]); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("err = %v, want ErrHoldsPointers", err)
	}
}

func TestSequentialInsertAndDelete(t *testing.T) {
	t.Parallel()

	tr := newTree(t, &memory.Local{})
	const n = 200_000
	for i := range uint64(n) {
		if err := tr.Set(i, i*2); err != nil {
			t.Fatal(err)
		}
	}
	if tr.Len() != n {
		t.Fatalf("Len = %d", tr.Len())
	}
	if k, v, ok := tr.Min(); !ok || k != 0 || v != 0 {
		t.Fatalf("Min = %d, %d, %v", k, v, ok)
	}
	if k, v, ok := tr.Max(); !ok || k != n-1 || v != 2*(n-1) {
		t.Fatalf("Max = %d, %d, %v", k, v, ok)
	}
	want := uint64(0)
	for k, v := range tr.All() {
		if k != want || v != k*2 {
			t.Fatalf("All at %d: %d -> %d", want, k, v)
		}
		want++
	}
	want = 12_345
	for k := range tr.From(want) {
		if k != want {
			t.Fatalf("From: %d, want %d", k, want)
		}
		want++
		if want == 12_400 {
			break
		}
	}
	for i := range uint64(n) {
		if !tr.Delete(i) {
			t.Fatalf("Delete(%d) missed", i)
		}
		if _, ok := tr.Get(i); ok {
			t.Fatalf("Get(%d) after Delete", i)
		}
	}
	if tr.Len() != 0 {
		t.Fatalf("Len = %d after deleting everything", tr.Len())
	}
	if _, _, ok := tr.Min(); ok {
		t.Fatal("Min found an entry in an empty tree")
	}
	if tr.Delete(1) {
		t.Fatal("Delete on an empty tree reported true")
	}
}

func TestStructuralChangeDuringAllPanics(t *testing.T) {
	t.Parallel()

	tr := newTree(t, &memory.Local{})
	for i := range uint64(100) {
		if err := tr.Set(i, i); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		msg, _ := recover().(string)
		if msg != "btree: modified during All" {
			t.Fatalf("panic = %q", msg)
		}
	}()
	for k := range tr.All() {
		tr.Delete(k)
	}
}

func TestFreeReleasesEverything(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	tr, err := btree.New[uint64, [4]uint64](a, cmp.Compare[uint64])
	if err != nil {
		t.Fatal(err)
	}
	for i := range uint64(50_000) {
		if err := tr.Set(i, [4]uint64{i}); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := tr.Ptr(777); !ok || got[0] != 777 {
		t.Fatalf("Ptr = %v, %v", got, ok)
	}
	if err := tr.Free(); err != nil {
		t.Fatal(err)
	}
}

func FuzzMatchesModel(f *testing.F) {
	f.Add(testkit.Seed(11, 1<<16))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a := &memory.Local{}
		defer a.Close()
		tr, err := btree.New[uint64, uint64](a, cmp.Compare[uint64])
		if err != nil {
			t.Fatal(err)
		}
		model := map[uint64]uint64{}
		ops := testkit.NewOps(data)
		for ops.More() {
			key := ops.Uint64() % 2048
			switch ops.Int(5) {
			case 0, 1:
				v := ops.Uint64()
				if err := tr.Set(key, v); err != nil {
					t.Fatal(err)
				}
				model[key] = v
			case 2, 3:
				_, want := model[key]
				if got := tr.Delete(key); got != want {
					t.Fatalf("Delete(%d) = %v, want %v", key, got, want)
				}
				delete(model, key)
			default:
				got, ok := tr.Get(key)
				want, wantOK := model[key]
				if ok != wantOK || got != want {
					t.Fatalf("Get(%d) = %d, %v; want %d, %v", key, got, ok, want, wantOK)
				}
			}
		}
		keys := make([]uint64, 0, len(model))
		for k := range model {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		i := 0
		for k, v := range tr.All() {
			if k != keys[i] || v != model[k] {
				t.Fatalf("All[%d] = %d -> %d, want %d -> %d", i, k, v, keys[i], model[keys[i]])
			}
			i++
		}
		if i != len(keys) || tr.Len() != len(keys) {
			t.Fatalf("All yielded %d, Len = %d, want %d", i, tr.Len(), len(keys))
		}
		from := ops.Uint64()%2048 + 3
		at, _ := slices.BinarySearch(keys, from)
		for k := range tr.From(from) {
			if at >= len(keys) || k != keys[at] {
				t.Fatalf("From(%d) yielded %d at position %d of %v", from, k, at, keys)
			}
			at++
		}
		if at != len(keys) {
			t.Fatalf("From(%d) stopped at %d of %d", from, at, len(keys))
		}
	})
}

func BenchmarkSetGetDelete(b *testing.B) {
	tr := newTree(b, &memory.Local{})
	var i uint64
	for b.Loop() {
		key := (i * 0x9e3779b97f4a7c15) >> 44
		if err := tr.Set(key, i); err != nil {
			b.Fatal(err)
		}
		tr.Get(key)
		if i&1 == 0 {
			tr.Delete(key)
		}
		i++
	}
}

func filled(t *testing.T, n int) *btree.Tree[int, int] {
	t.Helper()
	tree, err := btree.New[int, int](memtest.Local(t), cmp.Compare[int])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tree.Free(); err != nil {
			t.Error(err)
		}
	})
	for i := range n {
		if err := tree.Set(i*2, i); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

func TestRangeIsHalfOpen(t *testing.T) {
	t.Parallel()

	tree := filled(t, 1000)
	var got []int
	for k := range tree.Range(101, 111) {
		got = append(got, k)
	}
	if !slices.Equal(got, []int{102, 104, 106, 108, 110}) {
		t.Fatalf("Range = %v", got)
	}
	for k := range tree.Range(111, 101) {
		t.Fatalf("empty Range yielded %d", k)
	}
	seen := 0
	for range tree.Range(0, 1<<30) {
		seen++
		if seen == 3 {
			break
		}
	}
	if seen != 3 {
		t.Fatalf("early exit saw %d", seen)
	}
}

func TestBackwardMirrorsAll(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 1, 15, 16, 1000} {
		tree := filled(t, n)
		var forward, backward []int
		for k := range tree.All() {
			forward = append(forward, k)
		}
		for k := range tree.Backward() {
			backward = append(backward, k)
		}
		slices.Reverse(backward)
		if !slices.Equal(forward, backward) {
			t.Fatalf("n=%d: Backward does not mirror All", n)
		}
	}
	tree := filled(t, 100)
	for k := range tree.Backward() {
		if k != 198 {
			t.Fatalf("first key = %d", k)
		}
		break
	}
}

func TestKeysAndValuesFollowKeyOrder(t *testing.T) {
	t.Parallel()

	tree := filled(t, 100)
	i := 0
	for k := range tree.Keys() {
		if k != i*2 {
			t.Fatalf("key %d = %d", i, k)
		}
		i++
	}
	i = 0
	for v := range tree.Values() {
		if v != i {
			t.Fatalf("value %d = %d", i, v)
		}
		i++
	}
	if i != 100 {
		t.Fatalf("saw %d values", i)
	}
}

func TestBackwardPanicsOnModification(t *testing.T) {
	t.Parallel()

	tree := filled(t, 100)
	defer func() {
		if recover() == nil {
			t.Fatal("modifying during Backward did not panic")
		}
	}()
	for k := range tree.Backward() {
		tree.Delete(k)
	}
}
