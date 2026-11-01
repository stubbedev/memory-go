package ring_test

import (
	"errors"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/memtest"
	"github.com/stubbedev/memory-go/ring"
)

func TestFullAndEmpty(t *testing.T) {
	t.Parallel()

	q, err := ring.New[int](memtest.Local(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Free()
	if q.Cap() != 4 {
		t.Fatalf("Cap = %d, want 4", q.Cap())
	}
	if _, ok := q.Pop(); ok {
		t.Fatal("Pop on empty reported a value")
	}
	for round := range 3 {
		for i := range 4 {
			if !q.Push(round*10 + i) {
				t.Fatalf("Push %d failed", i)
			}
		}
		if q.Push(99) {
			t.Fatal("Push on full succeeded")
		}
		for i := range 4 {
			if got, ok := q.Pop(); !ok || got != round*10+i {
				t.Fatalf("Pop = %d, %v", got, ok)
			}
		}
	}
}

func TestRejectsBadArguments(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	if _, err := ring.New[int](a, -1); !errors.Is(err, memory.ErrNegativeLen) {
		t.Fatalf("err = %v", err)
	}
	if _, err := ring.New[string](a, 4); !errors.Is(err, memory.ErrHoldsPointers) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeliversInOrderAcrossGoroutines(t *testing.T) {
	t.Parallel()

	var a memory.Local
	defer a.Close()
	q, err := ring.New[uint64](&a, 64)
	if err != nil {
		t.Fatal(err)
	}
	const n = 2_000_000
	go func() {
		for i := uint64(0); i < n; {
			if q.Push(i) {
				i++
			}
		}
	}()
	for want := uint64(0); want < n; {
		if got, ok := q.Pop(); ok {
			if got != want {
				t.Errorf("Pop = %d, want %d", got, want)
				return
			}
			want++
		}
	}
}

func BenchmarkPushPop(b *testing.B) {
	q, err := ring.New[uint64](memtest.Local(b), 1024)
	if err != nil {
		b.Fatal(err)
	}
	defer q.Free()
	var i uint64
	for b.Loop() {
		q.Push(i)
		q.Pop()
		i++
	}
}

func BenchmarkAcrossGoroutines(b *testing.B) {
	q, err := ring.New[uint64](memtest.Local(b), 1<<14)
	if err != nil {
		b.Fatal(err)
	}
	defer q.Free()
	n := b.N
	done := make(chan struct{})
	go func() {
		defer close(done)
		for got := 0; got < n; {
			if _, ok := q.Pop(); ok {
				got++
			}
		}
	}()
	for i := 0; i < n; {
		if q.Push(uint64(i)) {
			i++
		}
	}
	<-done
}
