// Package memtest provides test helpers shared by the memory-go packages.
package memtest

import (
	"testing"

	memory "github.com/stubbedev/memory-go"
)

func Local(t testing.TB) *memory.Local {
	t.Helper()
	a := &memory.Local{}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return a
}
