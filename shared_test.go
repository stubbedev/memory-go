package memory_test

import (
	"sync"
	"testing"

	memory "github.com/stubbedev/memory-go"
)

func TestSharedFreesAcrossGoroutines(t *testing.T) {
	t.Parallel()

	var a memory.Shared
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	const producers, each = 4, 20_000
	ch := make(chan memory.Ptr[point], 256)
	var made sync.WaitGroup
	for g := range producers {
		made.Go(func() {
			for i := range each {
				p, err := memory.New[point](&a)
				if err != nil {
					t.Error(err)
					return
				}
				*p.Value() = point{X: int64(g), Y: int64(i)}
				ch <- p
			}
		})
	}
	go func() {
		made.Wait()
		close(ch)
	}()
	var freed sync.WaitGroup
	for range producers {
		freed.Go(func() {
			for p := range ch {
				if v := p.Value(); v.X < 0 || v.X >= producers {
					t.Errorf("value corrupted: %+v", *v)
				}
				if err := memory.Free(&a, p); err != nil {
					t.Error(err)
				}
			}
		})
	}
	freed.Wait()
	if s := a.Stats(); s != (memory.Stats{}) {
		t.Fatalf("Stats = %+v, want zero", s)
	}
	if err := a.Trim(); err != nil {
		t.Fatal(err)
	}
	if foot := a.Footprint(); foot.Committed != 0 {
		t.Fatalf("Committed after Trim = %d, want 0", foot.Committed)
	}
}

func TestSharedReusableAfterClose(t *testing.T) {
	t.Parallel()

	var a memory.Shared
	for range 3 {
		p, err := memory.New[point](&a)
		if err != nil {
			t.Fatal(err)
		}
		if *p.Value() != (point{}) {
			t.Fatalf("*p = %+v, want zero", *p.Value())
		}
		big, err := memory.MakeSlice[byte](&a, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		if big.Len() != 1<<20 {
			t.Fatal("wrong length")
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		if s := a.Stats(); s != (memory.Stats{}) {
			t.Fatalf("Stats after Close = %+v", s)
		}
	}
}

func BenchmarkSharedNewFreeParallel(b *testing.B) {
	var a memory.Shared
	b.Cleanup(func() {
		if err := a.Close(); err != nil {
			b.Error(err)
		}
	})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p, err := memory.New[point](&a)
			if err != nil {
				b.Error(err)
				return
			}
			if err := memory.Free(&a, p); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
