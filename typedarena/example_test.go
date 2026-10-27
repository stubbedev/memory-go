package typedarena_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/typedarena"
)

func Example() {
	var a memory.Local
	defer a.Close()

	type node struct {
		Value int
		Next  memory.Ref[node]
	}

	ta, err := typedarena.New[node](&a, 1024)
	if err != nil {
		panic(err)
	}
	defer ta.Free()

	var head memory.Ref[node]
	for i := range 3 {
		n, err := ta.Alloc()
		if err != nil {
			panic(err)
		}
		*n.Value() = node{Value: i, Next: head}
		head = n
	}
	for n := head; !n.IsNil(); n = n.Value().Next {
		fmt.Print(n.Value().Value, " ")
	}
	fmt.Println()
	// Output: 2 1 0
}
