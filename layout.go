package memory

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"
)

var (
	// ErrHoldsPointers reports a type that holds Go pointers and so cannot
	// live off-heap.
	ErrHoldsPointers = errors.New("type holds Go pointers")
	// ErrNegativeLen reports a negative length or capacity.
	ErrNegativeLen = errors.New("negative length")
	// ErrTooLarge reports a size that overflows int.
	ErrTooLarge = errors.New("allocation size overflows int")
	// ErrAlignment reports an alignment that is not a power of two up to 4 KiB.
	ErrAlignment = errors.New("invalid alignment")
)

type offHeap interface{ offHeap() }

var (
	offHeapType = reflect.TypeFor[offHeap]()
	pkgPath     = reflect.TypeFor[Stats]().PkgPath()
	verdictsMu  sync.Mutex
	verdicts    atomic.Pointer[map[uintptr]error]
	cleanTypes  [64]atomic.Uintptr
)

const typeKeyShift = 5

func typeKey[T any]() uintptr {
	var probe any = (*T)(nil)
	return (*[2]uintptr)(unsafe.Pointer(&probe))[0]
}

func checkType[T any]() error {
	key := typeKey[T]()
	entry := &cleanTypes[(key>>typeKeyShift)%uintptr(len(cleanTypes))]
	if entry.Load() == key {
		return nil
	}
	err := slowCheckType[T](key)
	if err == nil {
		entry.Store(key)
	}
	return err
}

func slowCheckType[T any](key uintptr) error {
	if m := verdicts.Load(); m != nil {
		if err, ok := (*m)[key]; ok {
			return err
		}
	}
	return recordVerdict(reflect.TypeFor[T](), key)
}

func recordVerdict(t reflect.Type, key uintptr) error {
	verdictsMu.Lock()
	defer verdictsMu.Unlock()
	old := map[uintptr]error{}
	if m := verdicts.Load(); m != nil {
		if err, ok := (*m)[key]; ok {
			return err
		}
		old = *m
	}
	next := make(map[uintptr]error, len(old)+1)
	maps.Copy(next, old)
	next[key] = verdict(t)
	verdicts.Store(&next)
	return next[key]
}

func verdict(t reflect.Type) error {
	switch path, bad := findPointer(t, ""); {
	case bad == nil:
		return nil
	case path == "":
		return fmt.Errorf("memory: %s: %w", t, ErrHoldsPointers)
	default:
		return fmt.Errorf("memory: %s: %s is %s: %w", t, path, bad, ErrHoldsPointers)
	}
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
	if err := checkType[T](); err != nil {
		return 0, err
	}
	var zero T
	size := int(unsafe.Sizeof(zero))
	if n < 0 || (size != 0 && n > math.MaxInt/size) {
		return 0, sizeError[T](n)
	}
	return sizeOf[T](n), nil
}

func sizeError[T any](n int) error {
	t := reflect.TypeFor[T]()
	if n < 0 {
		return fmt.Errorf("memory: %d×%s: %w", n, t, ErrNegativeLen)
	}
	return fmt.Errorf("memory: %d×%s: %w", n, t, ErrTooLarge)
}

func sizeOf[T any](n int) int {
	var zero T
	return max(n*int(unsafe.Sizeof(zero)), 1)
}

func alignOf[T any]() int {
	var zero T
	return int(unsafe.Alignof(zero))
}
