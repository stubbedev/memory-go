package heapslab_test

import (
	"runtime"
	"testing"

	"github.com/stubbedev/memory-go/heapslab"
)

const (
	batch     = 1024
	treeDepth = 20
	chunk     = 4096
)

var sink *node

func perAlloc(b *testing.B, n int) {
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/alloc")
}

func BenchmarkPoolGetPut(b *testing.B) {
	p, err := heapslab.NewPool[node](chunk)
	if err != nil {
		b.Fatal(err)
	}
	defer p.Free()
	for b.Loop() {
		it := p.Get()
		sink = it.Value()
		p.Put(it)
	}
}

func BenchmarkArenaAllocReset(b *testing.B) {
	a, err := heapslab.NewArena[node](chunk)
	if err != nil {
		b.Fatal(err)
	}
	defer a.Free()
	for b.Loop() {
		for range batch {
			sink = a.Alloc()
		}
		a.Reset()
	}
	perAlloc(b, batch)
}

func BenchmarkGoNew(b *testing.B) {
	for b.Loop() {
		for range batch {
			sink = new(node)
		}
	}
	perAlloc(b, batch)
}

func buildGoTree(depth, key int) *node {
	if depth == 0 {
		return nil
	}
	return &node{key: key, left: buildGoTree(depth-1, 2*key), right: buildGoTree(depth-1, 2*key+1)}
}

func buildArenaTree(a *heapslab.Arena[node], depth, key int) *node {
	if depth == 0 {
		return nil
	}
	n := a.Alloc()
	*n = node{key: key, left: buildArenaTree(a, depth-1, 2*key), right: buildArenaTree(a, depth-1, 2*key+1)}
	return n
}

func buildPoolTree(p *heapslab.Pool[node], depth, key int) *node {
	if depth == 0 {
		return nil
	}
	n := p.Get().Value()
	*n = node{key: key, left: buildPoolTree(p, depth-1, 2*key), right: buildPoolTree(p, depth-1, 2*key+1)}
	return n
}

func gcCycles(b *testing.B, root *node) {
	b.Helper()
	runtime.GC()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(root)
}

func BenchmarkGCWithGoTree(b *testing.B) {
	gcCycles(b, buildGoTree(treeDepth, 1))
}

func BenchmarkGCWithArenaTree(b *testing.B) {
	a, err := heapslab.NewArena[node](chunk)
	if err != nil {
		b.Fatal(err)
	}
	gcCycles(b, buildArenaTree(a, treeDepth, 1))
	runtime.KeepAlive(a)
}

func BenchmarkGCWithPoolTree(b *testing.B) {
	p, err := heapslab.NewPool[node](chunk)
	if err != nil {
		b.Fatal(err)
	}
	gcCycles(b, buildPoolTree(p, treeDepth, 1))
	runtime.KeepAlive(p)
}

func BenchmarkBuildGoTree(b *testing.B) {
	for b.Loop() {
		sink = buildGoTree(treeDepth, 1)
	}
	perAlloc(b, 1<<treeDepth-1)
}

func BenchmarkBuildArenaTree(b *testing.B) {
	a, err := heapslab.NewArena[node](chunk)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		sink = buildArenaTree(a, treeDepth, 1)
		a.Reset()
	}
	perAlloc(b, 1<<treeDepth-1)
}
