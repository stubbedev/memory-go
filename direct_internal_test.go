package memory

import (
	"testing"
	"unsafe"
)

func TestDirectRetentionStaysWithinBudget(t *testing.T) {
	t.Parallel()

	var d direct
	t.Cleanup(func() {
		if err := d.close(); err != nil {
			t.Error(err)
		}
	})
	const size = 1 << 20
	bases := make([]unsafe.Pointer, 0, 32)
	for range 32 {
		base, reserved, err := d.alloc(size)
		if err != nil {
			t.Fatal(err)
		}
		if reserved != size {
			t.Fatalf("reserved %d, want %d", reserved, size)
		}
		bases = append(bases, base)
	}
	for _, base := range bases[:31] {
		if err := d.free(base); err != nil {
			t.Fatal(err)
		}
	}
	if d.freedBytes > d.peak+retainSlack {
		t.Fatalf("retained %d bytes, over budget %d", d.freedBytes, d.peak+retainSlack)
	}
	hot := 0
	for _, m := range d.freed {
		if !m.cold {
			hot++
		}
	}
	if hot != hotBytes/size {
		t.Fatalf("%d retained mappings committed, want %d", hot, hotBytes/size)
	}
	if last := d.freed[len(d.freed)-1]; last.base != bases[30] || last.cold {
		t.Fatalf("newest freed mapping not retained committed: %+v", last)
	}
	if err := d.trim(); err != nil {
		t.Fatal(err)
	}
	if len(d.freed) != 0 || d.freedBytes != 0 {
		t.Fatalf("after trim: %d mappings, %d bytes retained", len(d.freed), d.freedBytes)
	}
	if _, ok := d.reserved(bases[31]); !ok {
		t.Fatal("trim released a live mapping")
	}
}

func TestDirectEvictsOldestBeyondBudget(t *testing.T) {
	t.Parallel()

	var d direct
	t.Cleanup(func() {
		if err := d.close(); err != nil {
			t.Error(err)
		}
	})
	const size = 8 << 20
	first, _, err := d.alloc(size)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.free(first); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		base, _, err := d.alloc(size + osPageSize)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.free(base); err != nil {
			t.Fatal(err)
		}
	}
	if d.freedBytes > d.peak+retainSlack {
		t.Fatalf("retained %d bytes, over budget %d", d.freedBytes, d.peak+retainSlack)
	}
	if d.freed[0].base == first {
		t.Fatal("oldest mapping survived eviction")
	}
}
