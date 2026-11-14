// Package memtest provides allocators for tests that fail the test on a leak.
package memtest

import (
	"testing"

	memory "github.com/stubbedev/memory-go"
)

type closer interface {
	memory.Allocator
	Close() error
}

func watch[A closer](t testing.TB, a A) A {
	t.Helper()
	t.Cleanup(func() {
		if s := a.Stats(); s.Live != 0 {
			t.Errorf("leaked %d allocations (%d bytes)", s.Live, s.Bytes)
		}
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return a
}

// Local returns an allocator that fails t if anything allocated from it is still
// live when t ends, and closes it.
func Local(t testing.TB) *memory.Local {
	t.Helper()
	return watch(t, &memory.Local{})
}

// Shared is Local for a Shared allocator.
func Shared(t testing.TB) *memory.Shared {
	t.Helper()
	return watch(t, &memory.Shared{})
}

// GuardedQuarantine is the quarantine budget of the allocators Guarded returns.
const GuardedQuarantine = 64 << 20

// Guarded is Local for a memory.Guarded allocator with a quarantine of
// GuardedQuarantine bytes, so a use after free or an overrun faults at the point
// of the mistake. Run a suite on it to find what the other allocators cannot
// catch.
func Guarded(t testing.TB) *memory.Guarded {
	t.Helper()
	return watch(t, memory.NewGuarded(GuardedQuarantine))
}
