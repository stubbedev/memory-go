package union_test

import (
	"fmt"
	"math"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/union"
	"github.com/stubbedev/memory-go/vector"
)

type shape interface{ area() float64 }

type circle struct{ R float64 }

func (c circle) area() float64 { return math.Pi * c.R * c.R }

type rect struct{ W, H float64 }

func (r rect) area() float64 { return r.W * r.H }

func Example() {
	var a memory.Local
	defer a.Close()

	var r union.Registry
	if _, err := union.Register[circle](&r); err != nil {
		panic(err)
	}
	if _, err := union.Register[rect](&r); err != nil {
		panic(err)
	}

	shapes, err := vector.New[union.Value](&a, 0)
	if err != nil {
		panic(err)
	}
	defer shapes.Free()
	for _, s := range []shape{circle{R: 1}, rect{W: 2, H: 3}} {
		v, err := union.StoreInterface(&a, &r, s)
		if err != nil {
			panic(err)
		}
		if err := shapes.Push(v); err != nil {
			panic(err)
		}
	}

	for v := range shapes.Values() {
		fmt.Printf("%.2f\n", union.Interface(&r, v).(shape).area())
		if err := union.Free(&a, v); err != nil {
			panic(err)
		}
	}
	// Output:
	// 3.14
	// 6.00
}
