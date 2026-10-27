package pool_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/pool"
)

func Example() {
	var a memory.Local
	defer a.Close()

	type conn struct {
		ID    uint32
		Bytes uint64
	}

	p, err := pool.New[conn](&a, 1024)
	if err != nil {
		panic(err)
	}
	defer p.Free()

	c, err := p.Get()
	if err != nil {
		panic(err)
	}
	c.Value().ID = 7
	fmt.Println(*c.Value(), p.Len())
	p.Put(c)

	reused, err := p.Get()
	if err != nil {
		panic(err)
	}
	fmt.Println(*reused.Value(), p.Len())
	// Output:
	// {7 0} 1
	// {0 0} 1
}
