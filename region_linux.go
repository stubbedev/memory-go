package memory

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// NewSharedRegion returns a Region of up to maxLen Ts backed by an anonymous
// memory file (memfd), on Linux only. Pass r.File() to a child process, for
// example in exec.Cmd.ExtraFiles, and open it there with NewRegionFile to share
// the elements; the memory lives until every process has closed it.
func NewSharedRegion[T any](maxLen int) (*Region[T], error) {
	fd, err := unix.MemfdCreate("memory-go region", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("memory: create shared region: %w", err)
	}
	f := os.NewFile(uintptr(fd), "memfd:memory-go region")
	r, err := NewRegionFile[T](f, maxLen)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return r, nil
}
