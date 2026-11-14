package memory_test

import (
	"os"
	"os/exec"
	"testing"

	memory "github.com/stubbedev/memory-go"
)

const sharedRegionChild = "MEMORY_SHARED_REGION_CHILD"

func TestSharedRegion(t *testing.T) {
	if os.Getenv(sharedRegionChild) != "" {
		sharedRegionChildMain(t)
		return
	}
	t.Parallel()

	r, err := memory.NewSharedRegion[uint64](1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRegion(t, r)
	if _, err := r.Grow(1 << 16); err != nil {
		t.Fatal(err)
	}
	r.Elems()[1<<16-1] = 41
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedRegion$")
	cmd.Env = append(os.Environ(), sharedRegionChild+"=1")
	cmd.ExtraFiles = []*os.File{r.File()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if got := r.Elems()[1<<16-1]; got != 42 {
		t.Fatalf("parent reads %d after the child's write, want 42", got)
	}
}

func sharedRegionChildMain(t *testing.T) {
	r, err := memory.NewRegionFile[uint64](os.NewFile(3, "shared"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRegion(t, r)
	if r.Len() != 1<<16 {
		t.Fatalf("child Len = %d", r.Len())
	}
	r.Elems()[1<<16-1]++
}
