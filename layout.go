package memory

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
)

var (
	// ErrHoldsPointers reports a type that holds Go pointers and so cannot
	// live off-heap.
	ErrHoldsPointers = errors.New("type holds Go pointers")
	// ErrNegativeLen reports a negative length or capacity.
	ErrNegativeLen = errors.New("negative length")
	// ErrTooLarge reports a size that overflows int.
	ErrTooLarge = errors.New("allocation size overflows int")
)

type offHeap interface{ offHeap() }

var (
	offHeapType = reflect.TypeFor[offHeap]()
	pkgPath     = reflect.TypeFor[Stats]().PkgPath()
	verdicts    sync.Map
)

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
	if t.PkgPath() == pkgPath && t.Implements(offHeapType) {
		return "", nil
	}
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
