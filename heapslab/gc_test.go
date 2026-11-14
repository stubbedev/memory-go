package heapslab_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/stubbedev/memory-go/heapslab"
)

type payload [64]byte

type holder struct {
	ref *payload
}

func track(t *testing.T, ref **payload) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	obj := new(payload)
	runtime.AddCleanup(obj, func(ch chan struct{}) { close(ch) }, done)
	*ref = obj
	return done
}

func collected(done <-chan struct{}) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		select {
		case <-done:
			return true
		case <-time.After(10 * time.Millisecond):
		}
	}
	return false
}

func stillAlive(t *testing.T, done <-chan struct{}) {
	t.Helper()
	runtime.GC()
	runtime.GC()
	select {
	case <-done:
		t.Fatal("referent collected while its holder was live")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPoolPutReleasesReferents(t *testing.T) {
	p, err := heapslab.NewPool[holder](8)
	if err != nil {
		t.Fatal(err)
	}
	it := p.Get()
	done := track(t, &it.Value().ref)
	stillAlive(t, done)
	p.Put(it)
	if !collected(done) {
		t.Fatal("referent of a value put back was not collected")
	}
	runtime.KeepAlive(p)
}

func TestPoolResetReleasesReferents(t *testing.T) {
	p, err := heapslab.NewPool[holder](8)
	if err != nil {
		t.Fatal(err)
	}
	done := track(t, &p.Get().Value().ref)
	stillAlive(t, done)
	p.Reset()
	if !collected(done) {
		t.Fatal("referent of a value reset was not collected")
	}
	runtime.KeepAlive(p)
}

func TestArenaResetReleasesReferents(t *testing.T) {
	a, err := heapslab.NewArena[holder](8)
	if err != nil {
		t.Fatal(err)
	}
	inChunk := track(t, &a.Alloc().ref)
	big := a.AllocSlice(100)
	inBig := track(t, &big[99].ref)
	stillAlive(t, inChunk)
	stillAlive(t, inBig)
	a.Reset()
	if !collected(inChunk) {
		t.Fatal("referent of a chunk value was not collected after Reset")
	}
	if !collected(inBig) {
		t.Fatal("referent of an oversized slice was not collected after Reset")
	}
	runtime.KeepAlive(a)
	runtime.KeepAlive(big)
}
