// Package memory allocates typed values outside the Go heap without cgo.
//
// Memory comes straight from the operating system: small allocations from
// size-classed spans, large ones from a mapping of their own, rounded up to whole
// pages. The garbage collector neither scans nor frees it, so every value placed there must be free of Go pointers: no
// pointers, slices, strings, maps, channels, functions or interfaces. Every
// allocation checks this and returns ErrHoldsPointers otherwise. The handle
// types of this package, Ptr, Ref, Slice, View, Index and String, refer only to memory
// the garbage collector does not manage and may be stored off-heap. So may
// Pinned, a Go pointer to an object a Pins keeps pinned on the Go heap, as cgo
// allows C memory to hold pointers to pinned Go objects. Owned frees a handle
// from a Shared once a small owner on the Go heap becomes unreachable.
//
// Local serves a single goroutine, Shared several without contending, and Arena
// frees whole groups of allocations at once. Region grows in place without moving.
//
// Misuse panics instead of corrupting memory. Every allocation is tracked in a
// side table or trailer recording its allocator, its size and whether it is live, so a
// double free, a free or resize through the wrong allocator, and reading a
// freed Ptr, Slice or String, or one that has since been resized, through its
// handle all panic, as does using a Local from
// several goroutines at once. A handle to memory that has since been freed
// and handed out again cannot be told apart from its new owner, and Refs and
// Go pointers obtained from handles are not checked.
//
// Building with the memory_unchecked tag compiles these checks out: no
// side table, no liveness or owner checks and no concurrency guard, roughly
// halving the cost of an allocation. Misuse then corrupts memory silently.
// The pointer-type check and Stats remain.
package memory
