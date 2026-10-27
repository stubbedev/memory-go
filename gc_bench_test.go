package memory_test

import (
	"runtime"
	"testing"

	memory "github.com/stubbedev/memory-go"
)

const treeDepth = 20

type goNode struct {
	key         int
	left, right *goNode
}

func buildGoTree(depth, key int) *goNode {
	if depth == 0 {
		return nil
	}
	return &goNode{key: key, left: buildGoTree(depth-1, 2*key), right: buildGoTree(depth-1, 2*key+1)}
}

type offNode struct {
	key         int
	left, right memory.Ptr[offNode]
}

func buildOffTree(b *testing.B, a memory.Allocator, depth, key int) memory.Ptr[offNode] {
	if depth == 0 {
		return memory.Ptr[offNode]{}
	}
	p, err := memory.New[offNode](a)
	if err != nil {
		b.Fatal(err)
	}
	*p.Value() = offNode{key: key, left: buildOffTree(b, a, depth-1, 2*key), right: buildOffTree(b, a, depth-1, 2*key+1)}
	return p
}

func BenchmarkGCWithGoTree(b *testing.B) {
	root := buildGoTree(treeDepth, 1)
	runtime.GC()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(root)
}

func BenchmarkGCWithOffHeapTree(b *testing.B) {
	var a memory.Local
	defer a.Close()
	root := buildOffTree(b, &a, treeDepth, 1)
	runtime.GC()
	for b.Loop() {
		runtime.GC()
	}
	runtime.KeepAlive(root)
}
