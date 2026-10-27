# memory-go dev tasks.

# List all recipes with their descriptions.
default:
    @just --list

# Run every release gate in order: vet, lint, test, race, build.
check: vet lint test race build

# Static analysis of every package with go vet.
vet:
    go vet ./...

# Lint every package; settings live in .golangci.yml.
lint:
    golangci-lint run

# Run the test suite for every package.
test:
    go test ./...

# Run the test suite under the race detector, which needs cgo.
race:
    CGO_ENABLED=1 go test -race ./...

# Compile-check every package; the output is discarded.
build:
    go build -o /dev/null ./...

# Fuzz every container against its Go model for the given time each.
fuzz time="30s":
    go test ./vector -run='^$' -fuzz='^FuzzMatchesSlice$' -fuzztime={{time}}
    go test ./hashmap -run='^$' -fuzz='^FuzzMatchesMap$' -fuzztime={{time}}
    go test ./linkedlist -run='^$' -fuzz='^FuzzMatchesSlice$' -fuzztime={{time}}
    go test ./minheap -run='^$' -fuzz='^FuzzMatchesSortedSlice$' -fuzztime={{time}}

# Run every benchmark, including the GC comparison.
bench:
    go test -run='^$' -bench=. ./...

# Format every Go source in place with gofmt.
fmt:
    gofmt -w .
