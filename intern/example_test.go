package intern_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/intern"
)

func Example() {
	var a memory.Local
	defer a.Close()

	t, err := intern.New(&a)
	if err != nil {
		panic(err)
	}
	first, _ := t.Intern("status")
	second, _ := t.Intern("status")
	fmt.Println(first == second, t.Text(first))
	// Output: true status
}
