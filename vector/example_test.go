package vector_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/vector"
)

func Example() {
	var a memory.Local
	defer a.Close()

	v, err := vector.New[int](&a, 0)
	if err != nil {
		panic(err)
	}
	defer v.Free()
	for i := range 5 {
		if err := v.Push(i * i); err != nil {
			panic(err)
		}
	}
	v.RemoveAt(0)
	last, _ := v.Pop()
	fmt.Println(v.Slice(), last, v.Len())
	// Output: [1 4 9] 16 3
}
