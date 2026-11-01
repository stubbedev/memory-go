package btree_test

import (
	"cmp"
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/btree"
)

func Example() {
	var a memory.Local
	defer a.Close()

	t, err := btree.New[int, string32](&a, cmp.Compare[int])
	if err != nil {
		panic(err)
	}
	for _, k := range []int{30, 10, 20} {
		if err := t.Set(k, string32{byte(k)}); err != nil {
			panic(err)
		}
	}
	for k := range t.From(15) {
		fmt.Println(k)
	}
	// Output:
	// 20
	// 30
}

type string32 [32]byte
