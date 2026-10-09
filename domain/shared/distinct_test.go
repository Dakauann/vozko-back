package shared

import (
	"reflect"
	"testing"
)

func TestDistinctTrimmedKeepsTheFirstOfEachValueInOrder(t *testing.T) {
	got := DistinctTrimmed([]string{" a", "b", "a ", "", "  ", "c", "b"})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DistinctTrimmed = %v, want %v", got, want)
	}
	if got := DistinctTrimmed(nil); len(got) != 0 {
		t.Fatalf("DistinctTrimmed(nil) = %v, want empty", got)
	}
}
