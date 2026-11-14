//go:build !memory_unchecked

package heapref

func checkRef(n int, i uint32, op string) {
	if i == 0 {
		panic("heapref: " + op + " of a nil Ref")
	}
	if int64(i) > int64(n) {
		panic("heapref: " + op + " of a Ref from another Table")
	}
}

func checkGen(have, want uint32, op string) {
	if have != want {
		panic("heapref: " + op + " of a Ref that was deleted or is from another Table")
	}
}
