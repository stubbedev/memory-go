package memory

import (
	"runtime"
	"sync/atomic"
)

// Ownable is a handle Owned can free: Ptr, Slice or String.
type Ownable interface {
	release(a Allocator) error
}

func (p Ptr[T]) release(a Allocator) error   { return Free(a, p) }
func (s Slice[T]) release(a Allocator) error { return FreeSlice(a, s) }
func (s String) release(a Allocator) error   { return FreeString(a, s) }

// Owned ties a handle allocated from a Shared to a small object on the Go
// heap and frees the handle when that object becomes unreachable, or earlier
// on Close. The collector frees it on a goroutine of its own, which is why
// only a Shared can own handles. A handle whose Shared has been closed since
// Own is left alone, as Close already released it.
type Owned[H Ownable] struct {
	w       owning[H]
	cleanup runtime.Cleanup
	closed  atomic.Bool
}

type owning[H Ownable] struct {
	h     H
	a     *Shared
	epoch uint64
}

func (o owning[H]) release() error { return o.a.releaseIn(o.epoch, o.h) }

// Own takes over h, which must come from a and be live, and returns its owner.
// Nothing else may free h afterwards.
func Own[H Ownable](a *Shared, h H) *Owned[H] {
	o := &Owned[H]{w: owning[H]{h: h, a: a, epoch: a.currentEpoch()}}
	o.cleanup = runtime.AddCleanup(o, func(w owning[H]) { _ = w.release() }, o.w)
	return o
}

// Handle returns the owned handle, or the zero handle after Close. It stays
// valid only while o is reachable, so keep o alive, for instance with
// runtime.KeepAlive, until the handle is no longer used.
func (o *Owned[H]) Handle() H {
	if o.closed.Load() {
		var zero H
		return zero
	}
	return o.w.h
}

// Close frees the handle now and cancels its release on collection. Closing o
// again does nothing.
func (o *Owned[H]) Close() error {
	if !o.closed.CompareAndSwap(false, true) {
		return nil
	}
	o.cleanup.Stop()
	err := o.w.release()
	runtime.KeepAlive(o)
	return err
}
