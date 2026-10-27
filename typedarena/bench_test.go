package typedarena_test

import (
	"testing"

	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/typedarena"
)

const batch = 1024

var sink *particle

func BenchmarkAllocReset(b *testing.B) {
	ta, err := typedarena.New[particle](memtest.Local(b), 4096)
	if err != nil {
		b.Fatal(err)
	}
	defer ta.Free()
	for b.Loop() {
		for range batch {
			r, err := ta.Alloc()
			if err != nil {
				b.Fatal(err)
			}
			sink = r.Value()
		}
		if err := ta.Reset(); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*batch), "ns/alloc")
}

func BenchmarkGoNew(b *testing.B) {
	for b.Loop() {
		for range batch {
			sink = new(particle)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*batch), "ns/alloc")
}
