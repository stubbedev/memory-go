package hashmap_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/hashmap"
)

func Example() {
	var a memory.Local
	defer a.Close()

	type pos struct{ X, Y int32 }

	m, err := hashmap.New[pos, uint8](&a, 16)
	if err != nil {
		panic(err)
	}
	defer m.Free()
	if err := m.Set(pos{1, 2}, 7); err != nil {
		panic(err)
	}
	if p, ok := m.Ptr(pos{1, 2}); ok {
		*p++
	}
	v, ok := m.Get(pos{1, 2})
	_, missing := m.Get(pos{2, 1})
	fmt.Println(v, ok, missing, m.Len())
	// Output: 8 true false 1
}
