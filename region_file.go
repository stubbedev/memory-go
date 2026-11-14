package memory

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"unsafe"
)

const (
	regionMagic      = "memgoreg"
	regionOrder      = 0x01020304
	regionVersion    = 1
	regionHeaderSize = int(unsafe.Sizeof(regionHeader{}))
)

var (
	// ErrNotRegionFile reports a file that does not hold a Region: wrong magic,
	// format version or byte order, or shorter than its recorded length.
	ErrNotRegionFile = errors.New("not a region file")
	// ErrRegionMismatch reports a region file written for an element type of a
	// different size, alignment or layout.
	ErrRegionMismatch = errors.New("region file holds a different element type")
)

type regionHeader struct {
	magic       [8]byte
	order       uint32
	version     uint32
	elemAlign   uint32
	_           uint32
	elemSize    uint64
	fingerprint uint64
	length      uint64
	_           [16]byte
}

type regionFile struct {
	f       *os.File
	section uintptr
}

func (h *regionHeader) bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(h)), regionHeaderSize)
}

func (r *Region[T]) header() *regionHeader { return (*regionHeader)(r.start()) }

func headerFor(t reflect.Type) regionHeader {
	h := regionHeader{
		order:       regionOrder,
		version:     regionVersion,
		elemAlign:   uint32(t.Align()),
		elemSize:    uint64(t.Size()),
		fingerprint: fingerprint(t),
	}
	copy(h.magic[:], regionMagic)
	return h
}

func fingerprint(t reflect.Type) uint64 {
	h := fnv.New64a()
	describe(h, t)
	return h.Sum64()
}

func describe(w io.Writer, t reflect.Type) {
	fmt.Fprintf(w, "%s/%d/%d(", t.Kind(), t.Size(), t.Align())
	switch t.Kind() {
	case reflect.Array:
		fmt.Fprintf(w, "%d*", t.Len())
		describe(w, t.Elem())
	case reflect.Struct:
		for f := range t.Fields() {
			fmt.Fprintf(w, "%s@%d:", f.Name, f.Offset)
			describe(w, f.Type)
		}
	}
	fmt.Fprint(w, ")")
}

func (h *regionHeader) match(name string, t reflect.Type, want *regionHeader) error {
	switch {
	case h.magic != want.magic:
		return fmt.Errorf("memory: %s: magic %q, want %q: %w", name, h.magic[:], regionMagic, ErrNotRegionFile)
	case h.order != want.order:
		return fmt.Errorf("memory: %s: written with the other byte order: %w", name, ErrNotRegionFile)
	case h.version != want.version:
		return fmt.Errorf("memory: %s: format version %d, want %d: %w", name, h.version, want.version, ErrNotRegionFile)
	case h.elemSize != want.elemSize:
		return fmt.Errorf("memory: %s: holds %d-byte elements, %s is %d bytes: %w", name, h.elemSize, t, want.elemSize, ErrRegionMismatch)
	case h.elemAlign != want.elemAlign:
		return fmt.Errorf("memory: %s: holds elements aligned to %d bytes, %s is aligned to %d: %w", name, h.elemAlign, t, want.elemAlign, ErrRegionMismatch)
	case h.fingerprint != want.fingerprint:
		return fmt.Errorf("memory: %s: field layout differs from %s: %w", name, t, ErrRegionMismatch)
	}
	return nil
}

func readRegionHeader(f *os.File, t reflect.Type) (regionHeader, int64, error) {
	want := headerFor(t)
	info, err := f.Stat()
	if err != nil {
		return want, 0, fmt.Errorf("memory: stat region file: %w", err)
	}
	if info.Size() == 0 {
		if _, err := f.WriteAt(want.bytes(), 0); err != nil {
			return want, 0, fmt.Errorf("memory: write region header: %w", err)
		}
		return want, int64(regionHeaderSize), nil
	}
	var got regionHeader
	if _, err := f.ReadAt(got.bytes(), 0); err != nil {
		if errors.Is(err, io.EOF) {
			return got, 0, fmt.Errorf("memory: %s: %d bytes is shorter than a region header: %w", f.Name(), info.Size(), ErrNotRegionFile)
		}
		return got, 0, fmt.Errorf("memory: read region header: %w", err)
	}
	return got, info.Size(), got.match(f.Name(), t, &want)
}

// OpenRegion opens the region file at path, creating it if it does not exist,
// like NewRegionFile. The Region owns the file and closes it in Close.
func OpenRegion[T any](path string, maxLen int) (*Region[T], error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("memory: open region: %w", err)
	}
	r, err := NewRegionFile[T](f, maxLen)
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return r, nil
}

// NewRegionFile maps f, which must be open for reading and writing, as a Region
// of up to maxLen Ts. An empty f gets a header recording the element type, and
// one that already holds a Region opens with the elements and length it had; a
// different element type returns ErrRegionMismatch and anything else
// ErrNotRegionFile. Address space for maxLen elements is reserved up front, so
// the elements never move, and the file grows and shrinks with the Region. Writes
// go straight to the file, so other processes mapping it see them; f may be a
// memfd or other shared memory file to share a Region with child processes.
// Only one process may Grow or Truncate it, and others see the length it had
// when they opened it. On success r owns f and closes it in Close; on error f is
// left open.
func NewRegionFile[T any](f *os.File, maxLen int) (*Region[T], error) {
	reserved, err := regionBytes[T](maxLen)
	if err != nil {
		return nil, err
	}
	h, fileSize, err := readRegionHeader(f, reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	if h.length > uint64(maxLen) {
		return nil, fmt.Errorf("memory: %s: holds %d elements, more than %d: %w", f.Name(), h.length, maxLen, ErrRegionFull)
	}
	n := int(h.length)
	data := fileSize - int64(regionHeaderSize)
	if data < int64(n)*int64(h.elemSize) {
		return nil, fmt.Errorf("memory: %s: %d bytes of data is shorter than its %d elements: %w", f.Name(), data, n, ErrNotRegionFile)
	}
	base, section, err := mapRegionFile(f, regionHeaderSize+reserved)
	if err != nil {
		return nil, fmt.Errorf("memory: map %d bytes of %s: %w", regionHeaderSize+reserved, f.Name(), err)
	}
	return &Region[T]{
		base:      (*T)(unsafe.Add(base, regionHeaderSize)),
		limit:     maxLen,
		n:         n,
		reserved:  reserved,
		committed: int(min(data, int64(reserved))),
		file:      &regionFile{f: f, section: section},
	}, nil
}

// File returns the file backing r, or nil if r is not backed by one. It stays
// owned by r.
func (r *Region[T]) File() *os.File {
	if r.file == nil {
		return nil
	}
	return r.file.f
}

// Sync writes the elements and length of a file-backed r through to stable
// storage, returning once they are durable. It does nothing for a Region not
// backed by a file.
func (r *Region[T]) Sync() error {
	if r.file == nil {
		return nil
	}
	start, size := pageSpan(r.start(), regionHeaderSize+r.committed)
	if err := syncRegionFile(start, size); err != nil {
		return fmt.Errorf("memory: sync %d bytes of %s: %w", size, r.file.f.Name(), err)
	}
	if err := r.file.f.Sync(); err != nil {
		return fmt.Errorf("memory: sync region file: %w", err)
	}
	return nil
}

func (r *Region[T]) closeFile() error {
	f := r.file.f
	var errs []error
	if err := unmapRegionFile(r.file, r.start(), regionHeaderSize+r.reserved, regionHeaderSize+r.committed); err != nil {
		errs = append(errs, fmt.Errorf("memory: unmap %s: %w", f.Name(), err))
	}
	if err := f.Close(); err != nil {
		errs = append(errs, fmt.Errorf("memory: close region file: %w", err))
	}
	return errors.Join(errs...)
}
