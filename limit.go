package memory

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// ErrOverLimit reports off-heap memory that alone exceeds a memory limit.
var ErrOverLimit = errors.New("off-heap memory exceeds the limit")

// Footprinter reports memory held from the operating system. Allocator and
// Region implement it.
type Footprinter interface {
	Footprint() Footprint
}

// SyncMemoryLimit keeps the process within total bytes by setting the Go
// runtime's soft memory limit, the same knob as GOMEMLIMIT, to total minus what
// sources hold committed. The runtime cannot see off-heap memory, so without this
// a limit protects only the Go heap and the process can overshoot it. The limit
// follows off-heap use only when called, so call it periodically and after large
// changes. It returns the limit set. If sources alone reach total it returns
// ErrOverLimit and leaves the limit as it was, because a limit near zero would make
// the collector run constantly without helping.
func SyncMemoryLimit(total int64, sources ...Footprinter) (int64, error) {
	var committed int64
	for _, s := range sources {
		committed += int64(s.Footprint().Committed)
	}
	if committed >= total {
		return 0, fmt.Errorf("memory: %d bytes committed against a limit of %d: %w", committed, total, ErrOverLimit)
	}
	limit := total - committed
	debug.SetMemoryLimit(limit)
	return limit, nil
}
