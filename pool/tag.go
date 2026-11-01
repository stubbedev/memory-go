//go:build !memory_unchecked

package pool

import (
	"fmt"
	"unsafe"
)

const live = 1

type tag struct{ word uint64 }

func (t *tag) checkLive(s unsafe.Pointer) {
	if t.word&live == 0 {
		panic(fmt.Sprintf("pool: use of %p after Put", s))
	}
}

func (t *tag) mark(id uint64) { t.word = id | live }

func (t *tag) unmark(s unsafe.Pointer, id uint64) {
	if t.word != id|live {
		panic(fmt.Sprintf("pool: Put of %p, which is not live in this pool", s))
	}
	t.word = id
}
