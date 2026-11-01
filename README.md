# memory-go

Typed manual memory management for Go without cgo: values and containers that
live outside the Go heap, so the garbage collector never scans or frees them.

Memory comes straight from the OS (`mmap`, `VirtualAlloc`) through an allocator
written for this module: size-classed spans for small values, a mapping each for
large ones. `CGO_ENABLED=0` builds work. The API follows
[mm-go](https://github.com/joetifa2003/mm-go), which does the same on top of C
`calloc` through cgo, but misuse panics instead of corrupting memory.

```sh
go get github.com/stubbedev/memory-go
```

## Usage

```go
var a memory.Local
defer a.Close()

type node struct {
	Key         int
	Name        memory.String
	Left, Right memory.Ptr[node]
}

n, err := memory.New[node](&a)
if err != nil {
	return err
}
n.Value().Key = 42

xs, err := vector.New[int](&a, 0)
if err != nil {
	return err
}
if err := xs.Push(42); err != nil {
	return err
}
```

## Why

A large pointer-rich structure costs the GC on every cycle, because it must
trace every pointer. Off-heap it costs nothing. Full `runtime.GC()` with a
balanced binary tree of 2²⁰ − 1 nodes alive (`just bench`, linux/amd64, 8 threads):

| Tree | GC cycle |
| --- | --- |
| `*goNode` on the Go heap | 3.64 ms |
| `memory.Ptr[offNode]` off-heap | 0.13 ms |

Allocation speed depends on which tool you use. General-purpose `New` and `Free`
cost more than Go's `new`. The typed `pool` and `typedarena` skip the allocator:
once a chunk has room, getting a value is a free-list pop or a bump of an index.
Per allocation (`just bench`, linux/amd64, 12 threads; this machine is noisy, so
read these as rough):

| Operation | Checked | `memory_unchecked` | Go |
| --- | --- | --- | --- |
| `pool` `Get` + `Put` | 7 ns | 5 ns | 15 ns (`new`) |
| `typedarena` `Alloc`, `Reset` every 1024 | 8 ns | 5 ns | 15 ns (`new`) |
| `Arena` `New`, `Reset` every 1024 | 26 ns | 17 ns | 15 ns (`new`) |
| `New` + `Free` | 57 ns | 29 ns | 15 ns (`new`) |
| `hashmap` get, `Int` hasher, 16K entries | 12 ns | 12 ns | |
| `hashmap` set + get | 164 ns | 77 ns | 80 ns (`map`) |
| `hashmap` set + get, `Int` hasher, presized | 79 ns | 70 ns | |

Go's numbers leave out the GC work its allocations cause later; the off-heap ones
have none.

Memory (`u64`-to-`u64`, or a 24-byte node):

| | memory-go | Go |
| --- | --- | --- |
| `hashmap` entry | 34 B at 8M entries, 19–39 B in general | 47 B |
| `New` of a 24-byte node | 24 B | 24 B plus GC headroom |
| `pool` of a 24-byte node | 32 B, 24 B unchecked | |
| Node with two 4-byte `Index` links | 16 B | |
| Vector pushed to 1 GiB | 1.05 GB peak RSS | 3.6 GB (`append`) |

## Handles

Off-heap memory may not hold Go pointers, since the GC cannot see them there.
Every allocation checks its type and returns `ErrHoldsPointers` naming the
offending field (`memory: main.user: .Name is string: type holds Go
pointers`). These handles point only into off-heap memory and may be stored
there:

| Handle | Owns | Created by | Released by |
| --- | --- | --- | --- |
| `Ptr[T]` | one `T` | `New` | `Free` |
| `Slice[T]` | `n` consecutive `T`s | `MakeSlice`, `ResizeSlice` | `FreeSlice` |
| `String` | UTF-8 bytes | `NewString`, `AppendString` | `FreeString` |
| `Ref[T]` | nothing | `Ptr.Ref`, `Slice.Ref`, `typedarena` | its owner |
| `View[T]` | nothing | `Slice.View` | its owner |
| `Index[T]` | nothing, in 4 bytes | `Region.Grow`, `Region.IndexAt` | its `Region` |
| `pool.Item[T]` | one `T` | `Pool.Get` | `Pool.Put` |

`Ptr.Value`, `Slice.Elems` and `String.String` hand out ordinary Go pointers,
slices and strings for access. Those stay valid until the handle is freed or
resized and must not outlive it.

## Reservation

An allocation of up to 8 KiB takes a slot from a size class, four to a doubling,
so rounding wastes at most a quarter. Slots of a power-of-two size are aligned to
it, up to 4 KiB. `MakeSliceAligned` asks for an alignment, and `NewUninit`,
`MakeSliceUninit`, `MakeSliceAlignedUninit` and `Pool.GetUninit` skip zeroing a value
about to be overwritten. `NewFrom` and `SliceFrom` allocate and copy in one call.

Anything larger gets its own mapping rounded up to whole OS pages, starting on a
page: a 1024×1024 RGBA buffer (4 MiB) reserves 4 MiB plus at most one page.
`Stats.Bytes` reports these reservations. On Linux, growing one remaps it instead
of copying, so no moment holds both copies, and shrinking unmaps the tail in place
on every Unix.

Freed large mappings are kept for reuse by an allocation of the same page count,
up to the allocator's peak of live large bytes plus 4 MiB; beyond that the oldest
are unmapped. Retired mappings stay committed only within a budget: the largest of
4 MiB, an eighth of live large bytes, and the newest mapping if it is at most 32
MiB, so a frame buffer stays warm while the old doublings of a growing vector do not
stay resident. The rest are decommitted except for their last page, which keeps
the freed trailer readable, and read as zero without being touched when reused.
`Trim` returns every retained mapping and empty span. `Stats` covers what is
allocated; `Footprint` covers what is held from the OS, which is what `Trim` and
memory limits care about.

### Huge pages

`UseHugePages` aligns later allocations of at least 2 MiB to 2 MiB and asks Linux
for huge pages, including where THP is set to `madvise`. Pointer chasing over 1 GiB
took 105 ns per step on 4 KiB pages and about 75 ns on huge pages here. It is
opt-in because such memory can become resident in 2 MiB steps, first touch can take
much longer when physical memory is fragmented, and growing an allocation copies
instead of remapping. `Region.UseHugePages` does the same for a region.

### Regions

`Region[T]` reserves address space for up to 2³²−2 elements and uses memory only
for those it holds, so it grows without ever moving and pointers into it stay valid.
An `Index[T]` names an element in four bytes and stays meaningful wherever the
region is mapped. `vector.NewStable` gives a vector the same property. Regions are
checked for bounds only.

### Prefaulting

`Populate` and `Region.Populate` make memory resident in one batch, so a
latency-sensitive section takes no page faults on it. Contents do not change.

### The runtime

The Go runtime cannot see off-heap memory, so `GOMEMLIMIT` alone lets a process
overshoot. `SyncMemoryLimit` sets the runtime's limit to a total minus what your
allocators hold committed; call it periodically. `SetProfile` records the call
stack of every live allocation in a `pprof` profile, for finding leaks.

For the final binary, profile-guided optimisation (`default.pgo`, see `go help
build`) inlines this module's hot paths past the normal budget, including the
liveness checks on handles.

## Panics instead of corruption

Every small allocation has an entry in a side table of its span, recording its
size and whether it is live, and every span records its allocator; a large
allocation has a 16-byte trailer. Neither sits between values, so checked builds
use the same slots as unchecked ones. These panic at the point of the mistake:

- freeing twice, or freeing, resizing or reading a handle that was already
  resized, even when the resize kept the same address
- freeing or resizing through a different allocator than the one that allocated
- reading a freed `Ptr`, `Slice` or `String` through the handle
- putting a `pool.Item` back twice or into another pool, or reading it after `Put`
- using one `Local` from several goroutines at once
- out-of-range indices in every container

Handing a `Mark` to `Release` after a `Reset` or `Free`, or from another arena,
panics too.

Not caught: a stale handle whose memory was freed and has since been handed out
again, `Ref`s, `View`s and `Index`es, and Go pointers or slices obtained from a
handle and kept after it was freed. An `Arena` panics on a handle past its mark but
not on one whose space was handed out again, and does not detect a double free.
`Release` frees everything allocated or resized since the `Mark`, so a container
from before the `Mark` that grew after it is gone too, and a `Vector` caches its
elements without rechecking them, so using one after that goes uncaught.
Reading through an old handle faults instead of panicking once its pages are
unmapped: after `Close` or `Trim`, once a freed large mapping leaves the retention
budget, after a large resize that remapped or shrank it, or on Windows once a span
has been decommitted. Freeing or resizing a large allocation is checked against the
allocator's own table of live mappings, so a stale or foreign large handle panics
even then, and in `memory_unchecked` builds too.

Allocation failures (out of memory, negative or overflowing sizes, pointer
types) come back as errors.

### Opting out

Build with `-tags memory_unchecked` to compile the guards out: no side table or
trailer, no liveness or owner checks and no concurrency guard. Misuse then corrupts memory
silently instead of panicking, so test without the tag first. The
pointer-type check and `Stats` remain. Only the final binary's build decides,
so a library depending on memory-go cannot turn the checks off for its users.
`memory.Checked` reports which build you are in. See [Why](#why) for what it
saves.

## Allocators

| Type | Use |
| --- | --- |
| `memory.Local` | One goroutine. Zero value is ready; must not be copied after use. |
| `memory.Shared` | Several goroutines. Each call works in a shard of its own, and memory freed on another shard returns to its owner without a lock. |
| `memory.Arena` | One goroutine. Mixed types bump-allocated from a parent's spans; `Mark`, `Release` and `Reset` free in bulk, and `WithArena` scopes one to a function. |

All satisfy `memory.Allocator`, which every function and container takes, so a
hashmap can live in an arena and vanish with it. `Stats` reports live allocations
and bytes, `Footprint` what is held from the OS, `Trim` returns retained empty pages
to the OS, and `Close` releases everything at once. An arena's `Footprint` and its
parent's add up to the true total, so pass both to `SyncMemoryLimit`.

## Packages

| Package | What it is |
| --- | --- |
| `memory` | Allocators, `Region`, and the `Ptr`, `Ref`, `Slice`, `View`, `Index` and `String` handles |
| `vector` | Growable slice: `Push`, `Pop`, `At`, `Ptr`, `Set`, `RemoveAt`, `DeleteFunc`, `Truncate`, `Reserve`, `Clip`, `All`, `Values`, `From`, and `Append` for bulk copies; `NewStable` never moves |
| `hashmap` | Swiss table: `Map` for comparable keys with an optional `Hasher` such as `Int`, `Strings` for text keys looked up without allocating; `Contains`, `Keys`, `Values`, `Clear`, `Clip`, a capacity hint, `Map.GetOrSet` and `Map.Upsert` that hash once, and `Map.GetMany` for batched lookups that overlap their cache misses |
| `btree` | Ordered map with `Min`, `Max`, `All`, `Keys`, `Values`, `From`, `Range` and `Backward` |
| `intern` | One copy of each distinct string, named by a 4-byte `Symbol` |
| `bufpool` | Recycled byte buffers of power-of-two sizes, aligned for direct I/O |
| `ring` | Bounded lock-free queue from one goroutine to one other |
| `linkedlist` | Doubly linked list in one pooled node vector linked by index; freed nodes are reused, `Clip` compacts |
| `minheap` | Binary heap ordered by a `less` func, with `DeleteFunc` and `Clip` |
| `typedarena` | Chunked bump allocator for one type, handing out `Ref`s; `Reset` reuses its warm chunks, `Free` releases them |
| `pool` | Free-list allocator for one type: `Get` and `Put` individual values without touching the allocator |
| `memtest` | `Local` and `Shared` allocators that fail a test on a leak |

Containers are not safe for concurrent use, even on a `Shared` allocator.
`memory.String` is not comparable, because `==` could only compare addresses; use
`hashmap.Strings` to key by text. Adding or removing entries of a `hashmap`, `btree`
or `linkedlist` while ranging over `All`, `From`, `Range`, `Backward`, `Keys` or `Values` panics instead of reading freed or reused slots.

## Testing failure paths

Allocation failures are hard to provoke, so `memory.NewFaulty` wraps an allocator and
fails it on demand: `FailAllocsAfter(n)` for out-of-memory, `FailFreesAfter(n)` for
the operating system refusing memory back. This module's own containers are swept
this way, failing each allocation and free in turn.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, test-unchecked, test-32bit, race, build |
| `just vet` | Static analysis with `go vet` |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...` |
| `just test-unchecked` | `go test ./...` with the guards compiled out |
| `just test-32bit` | `go test ./...` as a 32-bit program |
| `just race` | Tests under the race detector (needs cgo; the library itself never does) |
| `just build` | Compile-check every package |
| `just fuzz [time]` | Fuzz each allocator and container against its model, 30s each by default |
| `just bench` | All benchmarks, including the GC comparison |
| `just fmt` | Format every Go source in place |

Every dev tool (Go toolchain pinned to the version go.mod declares, gopls,
golangci-lint, delve, just, git, gh) comes from the flake: `direnv allow`
on entry, or `nix develop` explicitly. `GOTOOLCHAIN=local` keeps the compiler
from downloading itself.

## Nix

- `nix build` builds and tests the module (`nix/package.nix`; the dev shell
  is `nix/shell.nix`, both imported by `flake.nix`; update `vendorHash`
  whenever go.sum changes)
- `nix fmt` formats the nix files with alejandra

## CI

`.github/workflows/test.yml` runs vet and tests on Linux, macOS and Windows,
with and without `memory_unchecked`,
the race detector on Linux and macOS, the tests as a 32-bit program, cross-builds for
arm64, 386, FreeBSD and Windows on arm64, golangci-lint, and a minute of fuzzing per
allocator and container.
`.github/workflows/security.yml` runs CodeQL (Go and GitHub Actions), Grype and
govulncheck, uploading findings to code scanning; dependency review gates pull
requests. Dependabot keeps the module and the workflows current.

## Linting

`.golangci.yml` starts from the standard linter set and layers on bodyclose,
copyloopvar, errorlint, gosec, intrange, misspell, modernize, nolintlint,
perfsprint, revive (default rules plus `exported`), unconvert, unparam,
usestdlibvars and whitespace, with gofumpt and gci as formatters. No issue
caps: every finding fails the build. gosec's unsafe audit (G103) is waived only
for the files that form the boundary between typed values and raw memory, listed in
`.golangci.yml`.
