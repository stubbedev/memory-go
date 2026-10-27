package memory

import (
	"strings"
	"testing"
)

func TestLocalRejectsConcurrentEntry(t *testing.T) {
	t.Parallel()

	var l Local
	defer l.Close()
	l.busy.Store(true)
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "several goroutines") {
			t.Fatalf("panic = %q, want concurrent-use panic", msg)
		}
		l.busy.Store(false)
	}()
	_, _ = New[int](&l)
}
