package heapslab_test

import (
	"fmt"

	"github.com/stubbedev/memory-go/heapslab"
)

func Example() {
	type entry struct {
		Name string
		Tags []string
		Next *entry
	}

	a, err := heapslab.NewArena[entry](1024)
	if err != nil {
		panic(err)
	}
	defer a.Free()

	var head *entry
	for _, name := range []string{"a", "b", "c"} {
		e := a.Alloc()
		*e = entry{Name: name, Tags: []string{"tag-" + name}, Next: head}
		head = e
	}
	for e := head; e != nil; e = e.Next {
		fmt.Print(e.Name, ":", e.Tags[0], ";")
	}
	fmt.Println()
	a.Reset()

	p, err := heapslab.NewPool[entry](1024)
	if err != nil {
		panic(err)
	}
	defer p.Free()
	it := p.Get()
	it.Value().Name = "conn"
	fmt.Println(it.Value().Name, p.Len())
	p.Put(it)
	fmt.Printf("%q %d\n", p.Get().Value().Name, p.Len())
	// Output:
	// c:tag-c;b:tag-b;a:tag-a;
	// conn 1
	// "" 1
}
