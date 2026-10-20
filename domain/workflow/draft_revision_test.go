package workflow

import (
	"errors"
	"fmt"
	"testing"
)

func TestASaveWithoutTheRevisionItWasBasedOnIsRefused(t *testing.T) {
	for _, tc := range []struct {
		revision int
		refused  bool
	}{
		{revision: -1, refused: true},
		{revision: 0, refused: true},
		{revision: FirstDraftRevision, refused: false},
		{revision: 7, refused: false},
	} {
		err := RequireDraftRevision(tc.revision)
		if refused := errors.Is(err, ErrDraftRevisionRequired); refused != tc.refused {
			t.Fatalf("revision %d: err = %v, refused = %v, want %v", tc.revision, err, refused, tc.refused)
		}
	}
}

func TestADraftConflictNamesTheRevisionStoredNow(t *testing.T) {
	err := fmt.Errorf("save: %w", &DraftConflict{WorkflowID: "wf-1", Expected: 3, Current: 5})
	if !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("a draft conflict must match ErrDraftConflict: %v", err)
	}
	current, ok := CurrentDraftRevisionOf(err)
	if !ok || current != 5 {
		t.Fatalf("CurrentDraftRevisionOf = %d, %v, want 5, true", current, ok)
	}
	if _, ok := CurrentDraftRevisionOf(ErrWorkflowNotFound); ok {
		t.Fatal("another error must not read as a conflict")
	}
}
