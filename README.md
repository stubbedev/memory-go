# memory-go

Typed manual memory management for Go without cgo: values and containers that
live outside the Go heap, so the garbage collector never scans or frees them.

Memory comes from [modernc.org/memory](https://pkg.go.dev/modernc.org/memory),
a pure Go allocator that maps pages from the OS (`mmap`, `VirtualAlloc`)
directly. `CGO_ENABLED=0` builds work. The API follows
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

Allocation itself is slower than Go's: `New` + `Free` takes about 42 ns against
9 ns for `new`, and the hashmap's set + get takes 34 ns against 19 ns for a Go
map. About half of the allocation cost is the atomic guard that turns
concurrent use of a `Local` into a panic.

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

`Ptr.Value`, `Slice.Elems` and `String.String` hand out ordinary Go pointers,
slices and strings for access. Those stay valid until the handle is freed or
resized and must not outlive it.

## Panics instead of corruption

Every allocation carries a 16-byte header recording its allocator and whether
it is live. These panic at the point of the mistake:

- freeing twice, or resizing a handle that was already resized
- freeing or resizing through a different allocator than the one that allocated
- reading a freed `Ptr`, `Slice` or `String` through the handle
- using one `Local` from several goroutines at once
- out-of-range indices in every container

Not caught: a stale handle whose memory was freed and has since been handed
out again, `Ref`s, and Go pointers or slices obtained from a handle and kept
after it was freed. After `Close`, touching old handles faults instead of
panicking, since their pages are unmapped.

Allocation failures (out of memory, negative or overflowing sizes, pointer
types) come back as errors.

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
| `typedarena` | Chunked bump allocator for one type, handing out `Ref`s, freed all at once |

Containers are not safe for concurrent use, even on a `Shared` allocator.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, race, build |
| `just vet` | Static analysis with `go vet` |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...` |
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
