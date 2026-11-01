package pool_test

import (
	"errors"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/pool"
)

type particle struct {
	X, Y, VX, VY float32
	ID           uint64
}

func TestRejectsInvalidArguments(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := pool.New[particle](a, 0); !errors.Is(err, pool.ErrChunkSize) {
		t.Errorf("chunk 0: err = %v, want ErrChunkSize", err)
	}
	if _, err := pool.New[*particle](a, 8); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Errorf("pointer type: err = %v, want ErrHoldsPointers", err)
	}
}

func FuzzMatchesModel(f *testing.F) {
	f.Add(memtest.Seed(9, 1<<15))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a := memtest.Local(t)
		p, err := pool.New[particle](a, 1+len(data)%17)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Free()
		ops := memtest.NewOps(data)
		var items []pool.Item[particle]
		var ids []uint64
		for step := 0; ops.More(); step++ {
			if ops.Int(5) < 3 || len(items) == 0 {
				it, err := p.Get()
				if err != nil {
					t.Fatal(err)
				}
				if *it.Value() != (particle{}) {
					t.Fatalf("step %d: Get returned %+v, want zero", step, *it.Value())
				}
				id := ops.Uint64()
				it.Value().ID = id
				items, ids = append(items, it), append(ids, id)
			} else {
				i := ops.Int(len(items))
				p.Put(items[i])
				items[i], ids[i] = items[len(items)-1], ids[len(ids)-1]
				items, ids = items[:len(items)-1], ids[:len(ids)-1]
			}
			if p.Len() != len(items) {
				t.Fatalf("step %d: Len = %d, want %d", step, p.Len(), len(items))
			}
		}
		for i, it := range items {
			if got := it.Value().ID; got != ids[i] {
				t.Fatalf("item %d ID = %d, want %d", i, got, ids[i])
			}
		}
	})
}

func TestRecyclesWithoutAllocating(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := pool.New[particle](a, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	items := make([]pool.Item[particle], 64)
	for i := range items {
		if items[i], err = p.Get(); err != nil {
			t.Fatal(err)
		}
	}
	before := a.Stats()
	for range 10 {
		for _, it := range items {
			p.Put(it)
		}
		for i := range items {
			if items[i], err = p.Get(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if after := a.Stats(); after != before {
		t.Fatalf("recycling allocated: %+v -> %+v", before, after)
	}
}

func BenchmarkGetPut(b *testing.B) {
	p, err := pool.New[particle](memtest.Local(b), 4096)
	if err != nil {
		b.Fatal(err)
	}
	defer p.Free()
	for b.Loop() {
		it, err := p.Get()
		if err != nil {
			b.Fatal(err)
		}
		p.Put(it)
	}
}

var sink *particle

func BenchmarkGoNew(b *testing.B) {
	for b.Loop() {
		sink = new(particle)
	}
}

func TestPutZeroItemPanics(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := pool.New[int](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Free() })
	defer func() {
		r := recover()
		if msg, _ := r.(string); msg != "pool: Put of the zero Item" {
			t.Fatalf("panic = %v, want the zero-Item panic", r)
		}
	}()
	p.Put(pool.Item[int]{})
}

func TestSlotOverhead(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	const chunk = 1 << 16
	p, err := pool.New[[3]uint64](a, chunk)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	perValue := a.Stats().Bytes / chunk
	want := 24
	if memory.Checked {
		want += 8
	}
	if perValue != want {
		t.Fatalf("%d bytes per 24-byte value, want %d", perValue, want)
	}
}

func TestSmallTypesRecycle(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := pool.New[uint8](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	items := make([]pool.Item[uint8], 0, 32)
	for i := range 32 {
		it, err := p.Get()
		if err != nil {
			t.Fatal(err)
		}
		if *it.Value() != 0 {
			t.Fatalf("item %d not zeroed", i)
		}
		*it.Value() = 0xff
		items = append(items, it)
	}
	for _, it := range items {
		if *it.Value() != 0xff {
			t.Fatal("neighbouring item overwritten")
		}
		p.Put(it)
	}
	for range 32 {
		it, err := p.Get()
		if err != nil {
			t.Fatal(err)
		}
		if *it.Value() != 0 {
			t.Fatal("recycled item not zeroed")
		}
	}
}

func TestZeroSizeTypeRejected(t *testing.T) {
	t.Parallel()

	if _, err := pool.New[struct{}](memtest.Local(t), 4); !errors.Is(err, pool.ErrZeroSize) {
		t.Fatalf("err = %v, want ErrZeroSize", err)
	}
}

func TestGetUninitReusesWithoutClearing(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	p, err := pool.New[[2]uint64](a, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	it, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	*it.Value() = [2]uint64{1, 2}
	p.Put(it)
	again, err := p.GetUninit()
	if err != nil {
		t.Fatal(err)
	}
	if again.Value()[1] != 2 {
		t.Fatalf("value = %v, want its second word kept", *again.Value())
	}
}
