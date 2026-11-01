package memory_test

import (
	"reflect"
	"testing"

	memory "github.com/stubbedev/memory-go"
)

func TestStringCannotKeyAMapByAddress(t *testing.T) {
	t.Parallel()

	if reflect.TypeFor[memory.String]().Comparable() {
		t.Fatal("String is comparable, so a map keyed by it would compare addresses")
	}
}
