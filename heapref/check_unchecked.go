//go:build memory_unchecked

package heapref

func checkRef(int, uint32, string)    {}
func checkGen(uint32, uint32, string) {}
