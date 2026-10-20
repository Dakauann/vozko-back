package shared

import (
	"reflect"
	"testing"
)

func TestUniqueNonEmptyKeepsFirstOccurrencesInOrder(t *testing.T) {
	got := UniqueNonEmpty([]string{" b", "a", "", "b", "  ", "c", "a"})
	if want := []string{"b", "a", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UniqueNonEmpty = %v, want %v", got, want)
	}
	if got := UniqueNonEmpty(nil); len(got) != 0 {
		t.Fatalf("nil input = %v", got)
	}
}
