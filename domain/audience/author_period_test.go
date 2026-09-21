package audience

import (
	"errors"
	"testing"
	"time"
)

func timePtr(t time.Time) *time.Time { return &t }

func TestAuthorsInputAcceptsAPeriod(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(7 * 24 * time.Hour)

	in := AuthorsInput{WorkspaceID: "ws-1", From: timePtr(from), To: timePtr(to)}
	in.Normalize()
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	if !in.HasPeriod() {
		t.Fatal("a range must report itself as a period")
	}

	lifetime := AuthorsInput{WorkspaceID: "ws-1"}
	lifetime.Normalize()
	if lifetime.HasPeriod() {
		t.Fatal("no range is the lifetime ranking")
	}
}

func TestAuthorsInputOpenEndedPeriod(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	in := AuthorsInput{WorkspaceID: "ws-1", From: timePtr(from)}
	in.Normalize()
	if !in.HasPeriod() {
		t.Fatal("a from with no to is still a period")
	}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorsInputRejectsABadPeriod(t *testing.T) {
	from := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)

	inverted := AuthorsInput{WorkspaceID: "ws-1", From: timePtr(from), To: timePtr(to)}
	inverted.Normalize()
	if err := inverted.Validate(); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter for an inverted range", err)
	}

	huge := AuthorsInput{
		WorkspaceID: "ws-1",
		From:        timePtr(from.Add(-MaxAuthorRankingRange - time.Hour)),
		To:          timePtr(from),
	}
	huge.Normalize()
	if err := huge.Validate(); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter for an oversized range", err)
	}
}

func TestAuthorContainersInputAcceptsAPeriod(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(30 * 24 * time.Hour)

	in := AuthorContainersInput{WorkspaceID: "ws-1", AuthorExternalID: "ig-1", From: timePtr(from), To: timePtr(to)}
	in.Normalize()
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}

	inverted := AuthorContainersInput{WorkspaceID: "ws-1", AuthorExternalID: "ig-1", From: timePtr(to), To: timePtr(from)}
	inverted.Normalize()
	if err := inverted.Validate(); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("err = %v, want ErrInvalidFilter", err)
	}
}
