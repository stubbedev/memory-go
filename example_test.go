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
	*v.Value() = vec3{X: 1, Y: 2, Z: 3}
	fmt.Println(*v.Value())

	s, err := memory.MakeSlice[int](&a, 3)
	if err != nil {
		panic(err)
	}
	copy(s.Elems(), []int{10, 20, 30})
	if s, err = memory.ResizeSlice(&a, s, 4); err != nil {
		panic(err)
	}
	fmt.Println(s.Elems())

	_, err = memory.New[string](&a)
	fmt.Println(err)

	if err := memory.FreeSlice(&a, s); err != nil {
		panic(err)
	}
	if err := memory.Free(&a, v); err != nil {
		panic(err)
	}
	fmt.Println(a.Stats())
	// Output:
	// {1 2 3}
	// [10 20 30 0]
	// memory: string: type holds Go pointers
	// {0 0}
}

func Example_linkedValues() {
	var a memory.Local
	defer a.Close()

	type node struct {
		Name memory.String
		Next memory.Ptr[node]
	}

	var head memory.Ptr[node]
	for _, name := range []string{"c", "b", "a"} {
		n, err := memory.New[node](&a)
		if err != nil {
			panic(err)
		}
		label, err := memory.NewString(&a, name)
		if err != nil {
			panic(err)
		}
		*n.Value() = node{Name: label, Next: head}
		head = n
	}
	for n := head; !n.IsNil(); {
		fmt.Print(n.Value().Name, " ")
		next := n.Value().Next
		if err := memory.FreeString(&a, n.Value().Name); err != nil {
			panic(err)
		}
		if err := memory.Free(&a, n); err != nil {
			panic(err)
		}
		n = next
	}
	fmt.Println()
	// Output: a b c
}
