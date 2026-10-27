package minheap_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/minheap"
)

func Example() {
	var a memory.Local
	defer a.Close()

	type job struct {
		Priority int
		ID       uint32
	}

	h, err := minheap.New(&a, func(x, y job) bool { return x.Priority < y.Priority })
	if err != nil {
		panic(err)
	}
	defer h.Free()
	for i, p := range []int{5, 1, 3} {
		if err := h.Push(job{Priority: p, ID: uint32(i)}); err != nil {
			panic(err)
		}
	}
	for h.Len() > 0 {
		j, _ := h.Pop()
		fmt.Print(j.ID, " ")
	}
	fmt.Println()
	// Output: 1 2 0
}
