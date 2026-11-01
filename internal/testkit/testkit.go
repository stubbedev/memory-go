// Package testkit provides the fuzz and benchmark helpers shared by this module's tests.
package testkit

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"
)

// Seed returns n reproducible pseudo-random bytes for a fuzz seed corpus.
func Seed(seed uint64, n int) []byte {
	rng := rand.New(rand.NewPCG(seed, seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Uint32())
	}
	return b
}

// Ops decodes a fuzz input into a stream of operations. Reads past the end
// yield zeros.
type Ops struct{ data []byte }

// NewOps returns an Ops reading data.
func NewOps(data []byte) *Ops { return &Ops{data: data} }

// More reports whether unread input remains.
func (o *Ops) More() bool { return len(o.data) > 0 }

func (o *Ops) take(n int) []byte {
	var buf [8]byte
	k := copy(buf[:n], o.data)
	o.data = o.data[k:]
	return buf[:n]
}

// Int returns a value in [0, n). n must be in (0, 65536].
func (o *Ops) Int(n int) int { return int(binary.LittleEndian.Uint16(o.take(2))) % n }

// Uint64 returns the next eight bytes as a value.
func (o *Ops) Uint64() uint64 { return binary.LittleEndian.Uint64(o.take(8)) }

// ReportRSS reports how many bytes of resident memory the process gained since
// baseline, from Resident, as a benchmark metric. It reports nothing where
// Resident is unavailable.
func ReportRSS(b *testing.B, baseline int64) {
	b.Helper()
	if now := Resident(); now > 0 {
		b.ReportMetric(float64(now-baseline), "rss-bytes")
	}
}
