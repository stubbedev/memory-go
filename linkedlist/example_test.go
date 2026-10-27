package linkedlist_test

import (
	"fmt"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/linkedlist"
)

func Example() {
	var a memory.Local
	defer a.Close()

	l, err := linkedlist.New[rune](&a)
	if err != nil {
		panic(err)
	}
	defer l.Free()
	for _, r := range "bcd" {
		if err := l.PushBack(r); err != nil {
			panic(err)
		}
	}
	if err := l.PushFront('a'); err != nil {
		panic(err)
	}
	l.RemoveAt(2)
	for _, r := range l.All() {
		fmt.Print(string(r))
	}
	fmt.Println()
	// Output: abd
}
