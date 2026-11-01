package memory_test

import (
	"strings"
	"testing"
)

func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("panic = %v, want one containing %q", r, want)
		}
	}()
	f()
}
