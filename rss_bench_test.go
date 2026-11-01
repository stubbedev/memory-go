package memory_test

import (
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/internal/memtest"
	"github.com/stubbedev/memory-go/vector"
)

func BenchmarkRSSGrowVector256MiB(b *testing.B) {
	for b.Loop() {
		var a memory.Local
		base := memtest.Resident()
		v, err := vector.New[uint64](&a, 0)
		if err != nil {
			b.Fatal(err)
		}
		for i := range uint64(256 << 20 / 8) {
			if err := v.Push(i); err != nil {
				b.Fatal(err)
			}
		}
		memtest.ReportRSS(b, base)
		b.ReportMetric(float64(a.Footprint().Committed), "committed-bytes")
		if err := a.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRSSMap1MEntries(b *testing.B) {
	for b.Loop() {
		var a memory.Local
		base := memtest.Resident()
		m, err := hashmap.New[uint64, uint64](&a, 0)
		if err != nil {
			b.Fatal(err)
		}
		for i := range uint64(1 << 20) {
			if err := m.Set(i, i); err != nil {
				b.Fatal(err)
			}
		}
		memtest.ReportRSS(b, base)
		b.ReportMetric(float64(a.Stats().Bytes)/(1<<20), "bytes/entry")
		if err := a.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
