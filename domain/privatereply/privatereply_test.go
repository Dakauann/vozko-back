package privatereply

import (
	"errors"
	"testing"
	"time"
)

func TestDeadlineRules(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	recent, old := now.Add(-6*24*time.Hour), now.Add(-8*24*time.Hour)

	if err := CheckDeadline(&recent, now); err != nil {
		t.Fatalf("recent: %v", err)
	}
	if err := CheckDeadline(&old, now); !errors.Is(err, ErrExpired) {
		t.Fatalf("old: %v", err)
	}
	if err := CheckDeadline(nil, now); !errors.Is(err, ErrDeadlineUnknown) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestAnAttemptOrASendConsumesTheReply(t *testing.T) {
	for status, want := range map[Status]bool{StatusAttempted: true, StatusSent: true, StatusFailed: false} {
		if (&Record{Status: status}).Consumed() != want {
			t.Errorf("%s consumed = %v", status, !want)
		}
	}
}

func TestDeadlineIsTheCommentTimePlusTheWindow(t *testing.T) {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	if got := Deadline(&at); got == nil || !got.Equal(at.Add(Window)) {
		t.Fatalf("deadline = %v", got)
	}
	if Deadline(nil) != nil {
		t.Fatal("no comment time, no deadline")
	}
}
