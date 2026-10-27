package memory

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"unsafe"
)

var (
	ErrHoldsPointers = errors.New("type holds Go pointers")
	ErrNegativeLen   = errors.New("negative length")
	ErrTooLarge      = errors.New("allocation size overflows int")
)

var verdicts sync.Map

func checkType(t reflect.Type) error {
	if v, ok := verdicts.Load(t); ok {
		err, _ := v.(error)
		return err
	}
	var err error
	switch path, bad := findPointer(t, ""); {
	case bad == nil:
	case path == "":
		err = fmt.Errorf("memory: %s: %w", t, ErrHoldsPointers)
	default:
		err = fmt.Errorf("memory: %s: %s is %s: %w", t, path, bad, ErrHoldsPointers)
	}
	verdicts.Store(t, err)
	return err
}

func findPointer(t reflect.Type, path string) (string, reflect.Type) {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return "", nil
	case reflect.Array:
		if t.Len() == 0 {
			return "", nil
		}
		return findPointer(t.Elem(), path+"[i]")
	case reflect.Struct:
		for f := range t.Fields() {
			if p, bad := findPointer(f.Type, path+"."+f.Name); bad != nil {
				return p, bad
			}
		}
		return "", nil
	default:
		return path, t
	}
}

func byteSize[T any](n int) (int, error) {
	t := reflect.TypeFor[T]()
	if err := checkType(t); err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("memory: %d×%s: %w", n, t, ErrNegativeLen)
	}
	size := int(t.Size())
	if size != 0 && n > math.MaxInt/size {
		return 0, fmt.Errorf("memory: %d×%s: %w", n, t, ErrTooLarge)
	}
	return max(n*size, 1), nil
}

func New[T any](a Allocator) (*T, error) {
	size, err := byteSize[T](1)
	if err != nil {
		return nil, err
	}
	p, err := a.calloc(size)
	if err != nil {
		return nil, fmt.Errorf("memory: allocate %s: %w", reflect.TypeFor[T](), err)
	}
	return (*T)(p), nil
}

func Free[T any](a Allocator, p *T) error {
	if p == nil {
		return nil
	}
	if err := a.free(unsafe.Pointer(p)); err != nil {
		return fmt.Errorf("memory: free %s: %w", reflect.TypeFor[T](), err)
	}
	return nil
}

func MakeSlice[T any](a Allocator, n int) ([]T, error) {
	size, err := byteSize[T](n)
	if err != nil || n == 0 {
		return nil, err
	}
	p, err := a.calloc(size)
	if err != nil {
		return nil, fmt.Errorf("memory: allocate %d×%s: %w", n, reflect.TypeFor[T](), err)
	}
	return unsafe.Slice((*T)(p), n), nil
}

func ResizeSlice[T any](a Allocator, s []T, n int) ([]T, error) {
	old := cap(s)
	if old == 0 {
		return MakeSlice[T](a, n)
	}
	size, err := byteSize[T](n)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, FreeSlice(a, s)
	}
	p, err := a.realloc(unsafe.Pointer(unsafe.SliceData(s)), size)
	if err != nil {
		return nil, fmt.Errorf("memory: resize %d×%s to %d: %w", old, reflect.TypeFor[T](), n, err)
	}
	r := unsafe.Slice((*T)(p), n)
	if n > old {
		clear(r[old:])
	}
	return r, nil
}

func FreeSlice[T any](a Allocator, s []T) error {
	if cap(s) == 0 {
		return nil
	}
	if err := a.free(unsafe.Pointer(unsafe.SliceData(s))); err != nil {
		return fmt.Errorf("memory: free %d×%s: %w", cap(s), reflect.TypeFor[T](), err)
	}
	return nil
}
