package intern_test

import (
	"encoding/binary"
	"strings"
	"testing"

	memory "github.com/stubbedev/memory-go"
	"github.com/stubbedev/memory-go/intern"
	"github.com/stubbedev/memory-go/internal/testkit"
	"github.com/stubbedev/memory-go/memtest"
)

func TestSameTextSameSymbol(t *testing.T) {
	t.Parallel()

	tab, err := intern.New(memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tab.Free()
	a, err := tab.Intern("field")
	if err != nil {
		t.Fatal(err)
	}
	b, err := tab.Intern(strings.Clone("field"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := tab.Intern("other")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a == c || a == 0 {
		t.Fatalf("symbols %d, %d, %d", a, b, c)
	}
	if tab.Text(a) != "field" || tab.Text(c) != "other" {
		t.Fatalf("Text = %q, %q", tab.Text(a), tab.Text(c))
	}
	if tab.Len() != 2 {
		t.Fatalf("Len = %d", tab.Len())
	}
	if _, ok := tab.Lookup("absent"); ok {
		t.Fatal("Lookup found text never interned")
	}
	if got, ok := tab.Lookup("field"); !ok || got != a {
		t.Fatalf("Lookup = %d, %v", got, ok)
	}
}

func TestLongAndEmptyStrings(t *testing.T) {
	t.Parallel()

	tab, err := intern.New(memtest.Local(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tab.Free()
	long := strings.Repeat("x", 1<<20)
	for _, text := range []string{"", long, "", long} {
		s, err := tab.Intern(text)
		if err != nil {
			t.Fatal(err)
		}
		if tab.Text(s) != text {
			t.Fatalf("Text(%d) has length %d, want %d", s, len(tab.Text(s)), len(text))
		}
	}
	if tab.Len() != 2 {
		t.Fatalf("Len = %d, want 2", tab.Len())
	}
}

func FuzzMatchesModel(f *testing.F) {
	f.Add(testkit.Seed(7, 1<<14))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		a := &memory.Local{}
		defer a.Close()
		tab, err := intern.New(a)
		if err != nil {
			t.Fatal(err)
		}
		model := map[string]intern.Symbol{}
		ops := testkit.NewOps(data)
		for ops.More() {
			var raw [8]byte
			binary.LittleEndian.PutUint64(raw[:], ops.Uint64()%1024)
			text := string(raw[:ops.Int(9)])
			s, err := tab.Intern(text)
			if err != nil {
				t.Fatal(err)
			}
			if want, ok := model[text]; ok && want != s {
				t.Fatalf("Intern(%q) = %d, want %d", text, s, want)
			}
			model[text] = s
			if tab.Text(s) != text {
				t.Fatalf("Text(%d) = %q, want %q", s, tab.Text(s), text)
			}
		}
		if tab.Len() != len(model) {
			t.Fatalf("Len = %d, want %d", tab.Len(), len(model))
		}
		if err := tab.Free(); err != nil {
			t.Fatal(err)
		}
	})
}
