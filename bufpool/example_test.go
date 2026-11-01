package bufpool_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/bufpool"
)

func Example() {
	var a memory.Local
	defer a.Close()

	p, err := bufpool.New(&a, 1<<20)
	if err != nil {
		panic(err)
	}
	b, err := p.Get(5000)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(b.Bytes()), b.Cap())
	if err := p.Put(b); err != nil {
		panic(err)
	}
	// Output: 5000 8192
}
