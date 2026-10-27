# memory-go

Typed manual memory management for Go without cgo: values and containers that
live outside the Go heap, so the garbage collector never scans or frees them.

Memory comes from [modernc.org/memory](https://pkg.go.dev/modernc.org/memory),
a pure Go allocator that maps pages from the OS (`mmap`, `VirtualAlloc`)
directly. `CGO_ENABLED=0` builds work. The API follows
[mm-go](https://github.com/joetifa2003/mm-go), which does the same on top of C
`calloc` through cgo.

```sh
go get github.com/stubbedev/memory-go
```

## Usage

```go
var a memory.Local
defer a.Close()

type vec3 struct{ X, Y, Z float64 }

v, err := memory.New[vec3](&a)
if err != nil {
	return err
}
defer memory.Free(&a, v)
*v = vec3{X: 1, Y: 2, Z: 3}

xs, err := vector.New[int](&a, 0)
if err != nil {
	return err
}
defer xs.Free()
if err := xs.Push(42); err != nil {
	return err
}
```

## Rules

- **No Go pointers off-heap.** The GC cannot see memory it does not own, so a
  stored pointer, slice, string, map, channel, func or interface could be
  collected while still referenced. Every allocation checks the type and
  returns `ErrHoldsPointers` when it holds one; the error names the offending
  field (`memory: main.user: .Name is string: type holds Go pointers`). The
  verdict is cached per type.
- **Free what you allocate, exactly once.** `Free`/`FreeSlice` take what
  `New`/`MakeSlice`/`ResizeSlice` returned, not a reslice of it. Use after free
  and double free are memory corruption, not panics.
- **Pointers into containers are short-lived.** `Ptr`, `Slice` and anything
  that points into a container are invalid after the next call that may grow
  it.
- **Close releases everything.** `Close` unmaps every page of the allocator,
  invalidating all values and containers built on it.

## Allocators

| Type | Use |
| --- | --- |
| `memory.Local` | One goroutine. Zero value is ready; must not be copied after use. |
| `memory.Shared` | Several goroutines; a mutex serialises every call. |

Both satisfy `memory.Allocator`, which every function and container takes.
`Trim` returns retained empty pages to the OS.

## Packages

| Package | What it is |
| --- | --- |
| `memory` | `New`, `Free`, `MakeSlice`, `ResizeSlice`, `FreeSlice` |
| `vector` | Growable slice: `Push`, `Pop`, `At`, `Ptr`, `Set`, `RemoveAt`, `DeleteFunc`, `Truncate`, `Reserve`, `All` |
| `hashmap` | Open addressing with linear probing and backward-shift deletion, hashed with `hash/maphash` |
| `linkedlist` | Doubly linked list whose nodes sit in one pooled vector and link by index; freed nodes are reused |
| `minheap` | Binary heap ordered by a `less` func, with `DeleteFunc` |
| `typedarena` | Chunked bump allocator for one type, freed all at once |

Allocation failures come back as errors with context; out-of-range indices
panic like slice indexing does.

## Recipes

| Recipe | What it does |
| --- | --- |
| `just check` | Every release gate in order: vet, lint, test, build |
| `just vet` | Static analysis with `go vet` |
| `just lint` | `golangci-lint run` against the strict config in `.golangci.yml` |
| `just test` | `go test ./...` |
| `just build` | Compile-check every package |
| `just fmt` | Format every Go source in place |

`go test -race` needs `CGO_ENABLED=1`; the library itself never does.

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

`.github/workflows/security.yml` runs CodeQL (Go and GitHub Actions), Grype and
govulncheck, uploading findings to code scanning; dependency review gates
pull requests. Dependabot keeps the module and the workflows current.

## Linting

`.golangci.yml` starts from the standard linter set and layers on bodyclose,
copyloopvar, errorlint, gosec, intrange, misspell, modernize, nolintlint,
perfsprint, revive, unconvert, unparam, usestdlibvars and whitespace, with
gofumpt and gci as formatters. No issue caps: every finding fails the build.
gosec's unsafe audit (G103) is waived for `layout.go` only, the single place
where typed values meet raw memory.
