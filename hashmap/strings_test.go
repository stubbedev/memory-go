package hashmap_test

import (
	"encoding/binary"
	"maps"
	"strconv"
	"testing"

	"github.com/stubbedev/memory-go/hashmap"
	"github.com/stubbedev/memory-go/internal/memtest"
)

func TestStringsFindsByTextNotAddress(t *testing.T) {
	t.Parallel()

	a := memtest.Local(t)
	m, err := hashmap.NewStrings[int](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	first := "hel" + strconv.Itoa(0)
	if err := m.Set(first, 1); err != nil {
		t.Fatal(err)
	}
	second := "hel" + "0"
	if got, ok := m.Get(second); !ok || got != 1 {
		t.Fatalf("Get = %d, %v", got, ok)
	}
	if got, ok := m.GetBytes([]byte(second)); !ok || got != 1 {
		t.Fatalf("GetBytes = %d, %v", got, ok)
	}
	if err := m.Set("", 9); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Get(""); !ok || got != 9 {
		t.Fatalf("empty key: %d, %v", got, ok)
	}
}

func TestStringsGetDoesNotAllocate(t *testing.T) {
	a := memtest.Local(t)
	m, err := hashmap.NewStrings[int](a, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Free()
	if err := m.Set("key", 1); err != nil {
		t.Fatal(err)
	}
	key := []byte("key")
	if allocs := testing.AllocsPerRun(100, func() { _, _ = m.GetBytes(key) }); allocs != 0 {
		t.Fatalf("GetBytes allocated %v times", allocs)
	}
}

func FuzzStringsMatchesMap(f *testing.F) {
	f.Add(memtest.Seed(5, 1<<16))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a := memtest.Local(t)
		m, err := hashmap.NewStrings[uint64](a, 0)
		if err != nil {
			t.Fatal(err)
		}
		model := map[string]uint64{}
		ops := memtest.NewOps(data)
		for ops.More() {
			var raw [8]byte
			binary.LittleEndian.PutUint64(raw[:], ops.Uint64()%512)
			key := string(raw[:ops.Int(9)])
			switch ops.Int(5) {
			case 0, 1:
				v := ops.Uint64()
				if err := m.Set(key, v); err != nil {
					t.Fatal(err)
				}
				model[key] = v
			case 2:
				got, err := m.Delete(key)
				if err != nil {
					t.Fatal(err)
				}
				_, want := model[key]
				if got != want {
					t.Fatalf("Delete(%q) = %v, want %v", key, got, want)
				}
				delete(model, key)
			case 3:
				if err := m.Clip(); err != nil {
					t.Fatal(err)
				}
			default:
				got, ok := m.Get(key)
				want, wantOK := model[key]
				if ok != wantOK || got != want {
					t.Fatalf("Get(%q) = %d, %v; want %d, %v", key, got, ok, want, wantOK)
				}
			}
			if m.Len() != len(model) {
				t.Fatalf("Len = %d, want %d", m.Len(), len(model))
			}
		}
		got := maps.Collect(m.All())
		if !maps.Equal(got, model) {
			t.Fatalf("All = %v, want %v", got, model)
		}
		if err := m.Free(); err != nil {
			t.Fatal(err)
		}
	})
}
