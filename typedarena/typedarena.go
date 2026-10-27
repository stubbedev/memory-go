// Package typedarena provides a chunked bump allocator for a single type whose
// values live off the Go heap and are freed all at once.
package typedarena

import (
	"errors"
	"fmt"

	memory "github.com/stubbedev/memory-go"
)

var ErrChunkSize = errors.New("chunk size must be positive")

type Arena[T any] struct {
	alloc     memory.Allocator
	chunkSize int
	chunks    [][]T
	rest      []T
}

func New[T any](a memory.Allocator, chunkSize int) (*Arena[T], error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("typedarena: chunk size %d: %w", chunkSize, ErrChunkSize)
	}
	ta := &Arena[T]{alloc: a, chunkSize: chunkSize}
	if err := ta.refill(); err != nil {
		return nil, err
	}
	return ta, nil
}

func (ta *Arena[T]) chunk(n int) ([]T, error) {
	c, err := memory.MakeSlice[T](ta.alloc, n)
	if err != nil {
		return nil, err
	}
	ta.chunks = append(ta.chunks, c)
	return c, nil
}

func (ta *Arena[T]) refill() error {
	c, err := ta.chunk(ta.chunkSize)
	if err != nil {
		return err
	}
	ta.rest = c
	return nil
}

func (ta *Arena[T]) Alloc() (*T, error) {
	s, err := ta.AllocSlice(1)
	if err != nil {
		return nil, err
	}
	return &s[0], nil
}

func (ta *Arena[T]) AllocSlice(n int) ([]T, error) {
	if n < 0 || n > ta.chunkSize {
		return ta.chunk(n)
	}
	if n > len(ta.rest) {
		if err := ta.refill(); err != nil {
			return nil, err
		}
	}
	s := ta.rest[:n:n]
	ta.rest = ta.rest[n:]
	return s, nil
}

func (ta *Arena[T]) Free() error {
	errs := make([]error, 0, len(ta.chunks))
	for _, c := range ta.chunks {
		errs = append(errs, memory.FreeSlice(ta.alloc, c))
	}
	ta.chunks, ta.rest = nil, nil
	return errors.Join(errs...)
}
