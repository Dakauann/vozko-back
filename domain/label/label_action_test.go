package label

import (
	"errors"
	"testing"
)

func TestALabelActionDefaultsToAdding(t *testing.T) {
	for raw, want := range map[string]LabelAction{"": LabelActionAdd, "add": LabelActionAdd, " Remove ": LabelActionRemove} {
		got, err := ParseLabelAction(raw)
		if err != nil || got != want {
			t.Errorf("%q = %q, %v", raw, got, err)
		}
	}
}

func TestAnUnknownLabelActionIsRefused(t *testing.T) {
	if _, err := ParseLabelAction("toggle"); !errors.Is(err, ErrInvalidLabelAction) {
		t.Fatalf("err = %v", err)
	}
}
