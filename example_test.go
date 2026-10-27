package memory_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
)

func Example() {
	var a memory.Local
	defer a.Close()

	type vec3 struct{ X, Y, Z float64 }

	v, err := memory.New[vec3](&a)
	if err != nil {
		panic(err)
	}
	*v = vec3{X: 1, Y: 2, Z: 3}
	fmt.Println(*v)

	s, err := memory.MakeSlice[int](&a, 3)
	if err != nil {
		panic(err)
	}
	s[0], s[1], s[2] = 10, 20, 30
	s, err = memory.ResizeSlice(&a, s, 4)
	if err != nil {
		panic(err)
	}
	fmt.Println(s)

	_, err = memory.New[string](&a)
	fmt.Println(err)

	if err := memory.FreeSlice(&a, s); err != nil {
		panic(err)
	}
	if err := memory.Free(&a, v); err != nil {
		panic(err)
	}
	// Output:
	// {1 2 3}
	// [10 20 30 0]
	// memory: string: type holds Go pointers
}
