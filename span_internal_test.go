package memory

import (
	"testing"
)

func TestSizeClassesCoverEverySmallSizeTightly(t *testing.T) {
	t.Parallel()

	for size := 1; size <= smallMax; size++ {
		got := int(classes[classFor(size)].size)
		if got < size {
			t.Fatalf("size %d maps to class %d", size, got)
		}
		if size > wordAlign && got-size > max(size/4, wordAlign) {
			t.Fatalf("size %d maps to class %d, wasting over a quarter", size, got)
		}
	}
	if last := classes[numClasses-1].size; last != smallMax {
		t.Fatalf("largest class = %d, want %d", last, smallMax)
	}
}

func TestReciprocalMatchesDivisionForEverySlot(t *testing.T) {
	t.Parallel()

	for _, k := range classes {
		s := span{slot: k.size, inv: k.inv, data: k.data, slots: k.slots}
		for i := range k.slots {
			off := k.data + i*k.size
			if got := uint32(uint64(off-k.data) * uint64(s.inv) >> 32); got != i {
				t.Fatalf("class %d slot %d: reciprocal gives %d", k.size, i, got)
			}
		}
	}
}

func TestSlotsFitTheirSpanAndPowersOfTwoAreNaturallyAligned(t *testing.T) {
	t.Parallel()

	for _, k := range classes {
		if k.data+k.slots*k.size > spanSize {
			t.Errorf("class %d overruns its span", k.size)
		}
		if k.slots == 0 {
			t.Errorf("class %d has no slots", k.size)
		}
		if k.size&(k.size-1) == 0 && k.data%min(k.size, maxAlign) != 0 {
			t.Errorf("class %d starts at unaligned %d", k.size, k.data)
		}
	}
}

func TestEmptySpanServesAnotherClassZeroed(t *testing.T) {
	t.Parallel()

	var l Local
	defer l.Close()
	const big = 1 << 10
	ptrs := make([]Slice[byte], 0, 2048)
	for range cap(ptrs) {
		s, err := MakeSlice[byte](&l, big)
		if err != nil {
			t.Fatal(err)
		}
		for i := range s.Elems() {
			s.Elems()[i] = 0xff
		}
		ptrs = append(ptrs, s)
	}
	for _, s := range ptrs {
		if err := FreeSlice(&l, s); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 64 << 10 {
		p, err := New[[3]uint64](&l)
		if err != nil {
			t.Fatal(err)
		}
		if *p.Value() != ([3]uint64{}) {
			t.Fatalf("allocation %d not zeroed: %v", i, *p.Value())
		}
		p.Value()[0] = 1
	}
	if got := l.Stats().Bytes; got != 64<<10*24 {
		t.Fatalf("Bytes = %d, want exactly 24 per value", got)
	}
}

func TestTrimUnmapsEmptyChunks(t *testing.T) {
	t.Parallel()

	var l Local
	defer l.Close()
	vals := make([]Slice[byte], 0, 2*chunkSpans*spanSize/smallMax)
	for range cap(vals) {
		s, err := MakeSlice[byte](&l, smallMax)
		if err != nil {
			t.Fatal(err)
		}
		vals = append(vals, s)
	}
	if got := l.Footprint().Mapped; got < chunkSize {
		t.Fatalf("Mapped = %d, want at least one chunk", got)
	}
	for _, s := range vals {
		if err := FreeSlice(&l, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Trim(); err != nil {
		t.Fatal(err)
	}
	foot := l.Footprint()
	if foot.Mapped != 0 || foot.Committed != 0 || foot.Retained != 0 {
		t.Fatalf("Footprint after Trim = %+v, want zero", foot)
	}
	s, err := MakeSlice[byte](&l, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := FreeSlice(&l, s); err != nil {
		t.Fatal(err)
	}
}

func TestLentSpansBalanceTheParentAccount(t *testing.T) {
	t.Parallel()

	var l Local
	defer l.Close()
	a := NewArena(&l)
	for range 100_000 {
		if _, err := New[[4]uint64](a); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.Footprint().Committed; got < 100_000*32 {
		t.Fatalf("arena Committed = %d", got)
	}
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
	if err := l.Trim(); err != nil {
		t.Fatal(err)
	}
	if foot := l.Footprint(); foot.Committed != 0 || foot.Mapped != 0 {
		t.Fatalf("Footprint after Trim = %+v", foot)
	}
}
