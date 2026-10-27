// Package memory allocates typed values outside the Go heap without cgo.
//
// Memory comes from modernc.org/memory, a pure Go allocator that maps pages
// from the operating system directly. The garbage collector neither scans nor
// frees it, so every value placed there must be free of Go pointers: no
// pointers, slices, strings, maps, channels, functions or interfaces. Every
// allocation checks this and returns ErrHoldsPointers otherwise. The handle
// types of this package, Ptr, Ref, Slice and String, point only into memory
// the garbage collector does not manage and may be stored off-heap.
//
// Local serves a single goroutine; Shared serialises access for several.
//
// Misuse panics instead of corrupting memory. Every allocation carries a
// 16-byte header naming its allocator and whether it is live, so a double
// free, a free or resize through the wrong allocator, and reading a freed Ptr,
// Slice or String through its handle all panic, as does using a Local from
// several goroutines at once. A handle to memory that has since been freed
// and handed out again cannot be told apart from its new owner, and Refs and
// Go pointers obtained from handles are not checked.
package memory
