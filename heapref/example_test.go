package heapref_test

import (
	"fmt"
	"strings"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/heapref"
)

func Example() {
	var a memory.Local
	defer a.Close()

	var handlers heapref.Table[func(int) string]

	type timer struct {
		Deadline uint64
		OnFire   heapref.Ref[func(int) string]
	}

	prefix := "fired"
	onFire, err := handlers.Put(func(n int) string { return fmt.Sprint(prefix, " ", n) })
	if err != nil {
		panic(err)
	}
	t, err := memory.New[timer](&a)
	if err != nil {
		panic(err)
	}
	*t.Value() = timer{Deadline: 100, OnFire: onFire}

	fmt.Println(handlers.Get(t.Value().OnFire)(int(t.Value().Deadline)))
	handlers.Delete(t.Value().OnFire)
	fmt.Println(handlers.Len())
	// Output:
	// fired 100
	// 0
}

func ExampleWeak() {
	var cache heapref.Weak[strings.Builder]
	b := &strings.Builder{}
	r, err := cache.Put(b)
	if err != nil {
		panic(err)
	}
	cache.Get(r).WriteString("still alive")
	fmt.Println(b.String())
	// Output: still alive
}

func ExampleFuncs() {
	var ops heapref.Funcs[func(a, b int) int]
	add, _ := ops.Register(func(a, b int) int { return a + b })
	mul, _ := ops.Register(func(a, b int) int { return a * b })

	type instr struct {
		Op   heapref.FuncID[func(a, b int) int]
		A, B int32
	}
	prog := []instr{{add, 2, 3}, {mul, 4, 5}}
	for _, in := range prog {
		fmt.Println(ops.Get(in.Op)(int(in.A), int(in.B)))
	}
	// Output:
	// 5
	// 20
}
