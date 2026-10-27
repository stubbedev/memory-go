// Package linkedlist provides a doubly linked list whose nodes live off the Go
// heap in one contiguous pool and link to each other by index.
package linkedlist

import (
	"fmt"
	"iter"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/vector"
)

type link int

const none link = 0

func linkTo(i int) link { return link(i + 1) }

func (l link) index() int { return int(l) - 1 }

type node[T any] struct {
	value      T
	prev, next link
}

type List[T any] struct {
	nodes      *vector.Vector[node[T]]
	head, tail link
	spare      link
	n          int
}

func New[T any](a memory.Allocator) (*List[T], error) {
	nodes, err := vector.New[node[T]](a, 0)
	if err != nil {
		return nil, err
	}
	return &List[T]{nodes: nodes}, nil
}

func (l *List[T]) Len() int { return l.n }

func (l *List[T]) node(at link) *node[T] { return l.nodes.Ptr(at.index()) }

func (l *List[T]) acquire(value T) (link, error) {
	if l.spare != none {
		at := l.spare
		l.spare = l.node(at).next
		*l.node(at) = node[T]{value: value}
		return at, nil
	}
	if err := l.nodes.Push(node[T]{value: value}); err != nil {
		return none, err
	}
	return linkTo(l.nodes.Len() - 1), nil
}

func (l *List[T]) PushBack(value T) error {
	at, err := l.acquire(value)
	if err != nil {
		return err
	}
	l.node(at).prev = l.tail
	if l.tail == none {
		l.head = at
	} else {
		l.node(l.tail).next = at
	}
	l.tail = at
	l.n++
	return nil
}

func (l *List[T]) PushFront(value T) error {
	at, err := l.acquire(value)
	if err != nil {
		return err
	}
	l.node(at).next = l.head
	if l.head == none {
		l.tail = at
	} else {
		l.node(l.head).prev = at
	}
	l.head = at
	l.n++
	return nil
}

func (l *List[T]) unlink(at link) T {
	nd := l.node(at)
	value, prev, next := nd.value, nd.prev, nd.next
	if prev == none {
		l.head = next
	} else {
		l.node(prev).next = next
	}
	if next == none {
		l.tail = prev
	} else {
		l.node(next).prev = prev
	}
	*nd = node[T]{next: l.spare}
	l.spare = at
	l.n--
	return value
}

func (l *List[T]) PopFront() (T, bool) {
	if l.head == none {
		var zero T
		return zero, false
	}
	return l.unlink(l.head), true
}

func (l *List[T]) PopBack() (T, bool) {
	if l.tail == none {
		var zero T
		return zero, false
	}
	return l.unlink(l.tail), true
}

func (l *List[T]) seek(i int) link {
	if i < 0 || i >= l.n {
		panic(fmt.Sprintf("linkedlist: index out of range [%d] with length %d", i, l.n))
	}
	if i < l.n/2 {
		at := l.head
		for range i {
			at = l.node(at).next
		}
		return at
	}
	at := l.tail
	for range l.n - 1 - i {
		at = l.node(at).prev
	}
	return at
}

func (l *List[T]) At(i int) T { return l.node(l.seek(i)).value }

func (l *List[T]) Ptr(i int) *T { return &l.node(l.seek(i)).value }

func (l *List[T]) RemoveAt(i int) T { return l.unlink(l.seek(i)) }

func (l *List[T]) IndexFunc(match func(T) bool) int {
	for i, value := range l.All() {
		if match(value) {
			return i
		}
	}
	return -1
}

func (l *List[T]) DeleteFunc(del func(T) bool) int {
	removed := 0
	for at := l.head; at != none; {
		next := l.node(at).next
		if del(l.node(at).value) {
			l.unlink(at)
			removed++
		}
		at = next
	}
	return removed
}

func (l *List[T]) All() iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		i := 0
		for at := l.head; at != none; at = l.node(at).next {
			if !yield(i, l.node(at).value) {
				return
			}
			i++
		}
	}
}

func (l *List[T]) Free() error {
	l.head, l.tail, l.spare, l.n = none, none, none, 0
	return l.nodes.Free()
}
