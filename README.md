# memory-go

Typed manual memory management for Go without cgo: values and containers that
live outside the Go heap, so the garbage collector never scans or frees them.

Small allocations come from [modernc.org/memory](https://pkg.go.dev/modernc.org/memory),
a pure Go allocator that maps pages from the OS (`mmap`, `VirtualAlloc`)
directly; large ones are mapped from the OS by memory-go itself (see
[Reservation](#reservation)). `CGO_ENABLED=0` builds work. The API follows
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

Allocation speed depends on which tool you use. General-purpose `New` and
`Free` go through modernc's size classes and are slower than Go's `new`. The
typed `pool` and `typedarena` skip that: once a chunk has room, getting a value
is a free-list pop or a bump of an index, and they beat `new` even with the
misuse guards on. Per allocation (`just bench`, linux/amd64, 8 threads, this
machine is noisy so read these as rough):

| Operation | Checked | `memory_unchecked` | Go `new` |
| --- | --- | --- | --- |
| `pool` `Get` + `Put` | 4–5 ns | 4 ns | 10 ns |
| `typedarena` `Alloc`, `Reset` every 1024 | 4.2 ns | 3.5 ns | 12.5 ns |
| `New` + `Free` | 43 ns | 21–26 ns | 10 ns |
| hashmap set + get | 36–45 ns | 36 ns | 19–26 ns (map) |

Go's numbers leave out the GC work its allocations cause later; the off-heap
ones have none.

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
| `pool.Item[T]` | one `T` | `Pool.Get` | `Pool.Put` |

`Ptr.Value`, `Slice.Elems` and `String.String` hand out ordinary Go pointers,
slices and strings for access. Those stay valid until the handle is freed or
resized and must not outlive it.

## Reservation

An allocation of up to 8 KiB with its header (or one OS page, if larger) takes
a slot of the next power of two in modernc's size classes. Anything larger
gets its own mapping rounded up to whole OS pages, so it reserves less than
one page beyond its size and header: a 1024×1024 RGBA buffer (4 MiB) reserves
4 MiB plus one page, not the 8 MiB a power-of-two class would. `Stats.Bytes`
reports these reservations.

Freed large mappings are kept for reuse by an allocation of the same page
count, up to the allocator's peak of live large bytes plus 4 MiB; beyond that
the oldest are unmapped. The newest 4 MiB of each size (at least one mapping)
stay committed; older ones are decommitted except for their first page, which
keeps the freed header readable. `Trim` unmaps every kept mapping.

## Panics instead of corruption

Every allocation carries a 16-byte header recording its allocator, its size
and whether it is live. These panic at the point of the mistake:

- freeing twice, or freeing, resizing or reading a handle that was already
  resized, even when the resize kept the same address
- freeing or resizing through a different allocator than the one that allocated
- reading a freed `Ptr`, `Slice` or `String` through the handle
- putting a `pool.Item` back twice or into another pool, or reading it after `Put`
- using one `Local` from several goroutines at once
- out-of-range indices in every container

Not caught: a stale handle whose memory was freed and has since been handed
out again, `Ref`s, and Go pointers or slices obtained from a handle and kept
after it was freed. After `Close` or `Trim`, or once a freed large mapping is
unmapped to stay within the retention budget, reading through an old handle
faults instead of panicking, since its pages are unmapped. Freeing or
resizing a large allocation is checked against the allocator's own table of
live mappings, so a stale or foreign large handle panics even then, and in
`memory_unchecked` builds too.

Allocation failures (out of memory, negative or overflowing sizes, pointer
types) come back as errors.

### Opting out

Build with `-tags memory_unchecked` to compile the guards out: no header, no
liveness or owner checks and no concurrency guard. Misuse then corrupts memory
silently instead of panicking, so test without the tag first. The
pointer-type check and `Stats` remain. Only the final binary's build decides,
so a library depending on memory-go cannot turn the checks off for its users.
`memory.Checked` reports which build you are in. See [Why](#why) for what it
saves.

## Allocators

| Type | Use |
| --- | --- |
| `memory.Local` | One goroutine. Zero value is ready; must not be copied after use. |
| `memory.Shared` | Several goroutines; a mutex serialises every call. |

Both satisfy `memory.Allocator`, which every function and container takes.
`Stats` reports live allocations and bytes, `Trim` returns retained empty pages
to the OS, and `Close` releases everything at once.

## Packages

| Package | What it is |
| --- | --- |
| `memory` | Allocators and the `Ptr`, `Ref`, `Slice` and `String` handles |
| `vector` | Growable slice: `Push`, `Pop`, `At`, `Ptr`, `Set`, `RemoveAt`, `DeleteFunc`, `Truncate`, `Reserve`, `Clip`, `All` |
| `hashmap` | Linear probing with backward-shift deletion and stored hashes; capacity hint, `Clip` |
| `linkedlist` | Doubly linked list in one pooled node vector linked by index; freed nodes are reused, `Clip` compacts |
| `minheap` | Binary heap ordered by a `less` func, with `DeleteFunc` and `Clip` |
| `typedarena` | Chunked bump allocator for one type, handing out `Ref`s; `Reset` reuses its warm chunks, `Free` releases them |
| `pool` | Free-list allocator for one type: `Get` and `Put` individual values without touching the allocator |

Containers are not safe for concurrent use, even on a `Shared` allocator.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, test-unchecked, race, build |
| `just vet` | Static analysis with `go vet` |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...` |
| `just test-unchecked` | `go test ./...` with the guards compiled out |
| `just race` | Tests under the race detector (needs cgo; the library itself never does) |
| `just build` | Compile-check every package |
| `just fuzz [time]` | Fuzz each container against its Go model, 30s each by default |
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
the race detector on Linux and macOS, cross-builds for arm64, 386, FreeBSD and
Windows on arm64, golangci-lint, and a minute of fuzzing per container.
`.github/workflows/security.yml` runs CodeQL (Go and GitHub Actions), Grype and
govulncheck, uploading findings to code scanning; dependency review gates pull
requests. Dependabot keeps the module and the workflows current.

## Linting

`.golangci.yml` starts from the standard linter set and layers on bodyclose,
copyloopvar, errorlint, gosec, intrange, misspell, modernize, nolintlint,
perfsprint, revive (default rules plus `exported`), unconvert, unparam,
usestdlibvars and whitespace, with gofumpt and gci as formatters. No issue
caps: every finding fails the build. gosec's unsafe audit (G103) is waived only
for `allocator.go`, `memory.go` and `string.go`, the boundary between typed
values and raw memory.
