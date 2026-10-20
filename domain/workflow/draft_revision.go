package workflow

import (
	"errors"
	"fmt"
)

const (
	FirstDraftRevision = 1
	DraftConflictCode  = "draft_conflict"
)

var (
	ErrDraftConflict         = errors.New("workflow: the draft was saved by someone else since it was read")
	ErrDraftRevisionRequired = errors.New("workflow: the draft revision the change is based on is required")
)

type DraftConflict struct {
	WorkflowID string
	Expected   int
	Current    int
}

func (c *DraftConflict) Error() string {
	return fmt.Sprintf("%v: workflow %s was edited at revision %d, the draft is at revision %d", ErrDraftConflict, c.WorkflowID, c.Expected, c.Current)
}

func (c *DraftConflict) Unwrap() error { return ErrDraftConflict }

func CurrentDraftRevisionOf(err error) (int, bool) {
	var conflict *DraftConflict
	if !errors.As(err, &conflict) {
		return 0, false
	}
	return conflict.Current, true
}

func RequireDraftRevision(revision int) error {
	if revision < FirstDraftRevision {
		return ErrDraftRevisionRequired
	}
	return nil
}
