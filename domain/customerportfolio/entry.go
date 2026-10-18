package customerportfolio

import (
	"strings"
	"time"
)

type Entry struct {
	ID          string
	WorkspaceID string
	HolderID    string
	Subject     Subject
	AddedBy     string
	AddedAt     time.Time
	UpdatedAt   time.Time
}

func New(workspaceID, holderID, addedBy string, subject Subject, at time.Time) (Entry, error) {
	workspaceID, holderID = strings.TrimSpace(workspaceID), strings.TrimSpace(holderID)
	if workspaceID == "" {
		return Entry{}, ErrWorkspaceRequired
	}
	if holderID == "" {
		return Entry{}, ErrHolderRequired
	}
	subject = subject.normalized()
	if err := subject.Validate(); err != nil {
		return Entry{}, err
	}
	return Entry{WorkspaceID: workspaceID, HolderID: holderID, Subject: subject, AddedBy: strings.TrimSpace(addedBy), AddedAt: at, UpdatedAt: at}, nil
}

func (e Entry) MoveTo(holderID string, at time.Time) (Entry, bool, error) {
	holderID = strings.TrimSpace(holderID)
	if holderID == "" {
		return e, false, ErrHolderRequired
	}
	if holderID == e.HolderID {
		return e, false, nil
	}
	e.HolderID, e.UpdatedAt = holderID, at
	return e, true, nil
}

func Claim(existing *Entry, wanted Entry) (Entry, bool, error) {
	if existing == nil {
		return wanted, true, nil
	}
	if existing.HolderID == wanted.HolderID {
		return *existing, false, nil
	}
	return Entry{}, false, &HeldByOther{Entry: *existing}
}
