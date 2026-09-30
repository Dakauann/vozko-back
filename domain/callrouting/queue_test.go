package callrouting

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func validQueue() Queue {
	return Queue{
		WorkspaceID:  "ws1",
		Name:         "Vendas",
		DepartmentID: "dept-vendas",
	}
}

func TestANewQueueGetsIndustryDefaults(t *testing.T) {
	q := validQueue()
	q.ApplyDefaults()
	if q.Strategy != StrategyLongestIdle || q.RingSeconds != 15 || q.MaxWaitSeconds != 300 || q.WrapUpSeconds != 10 {
		t.Fatalf("defaults = %+v", q)
	}
	if q.HoldMusic.PresetID != DefaultHoldPreset {
		t.Fatalf("hold music default = %+v", q.HoldMusic)
	}
	if err := q.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if q.RingTimeout() != 15*time.Second || q.MaxWait() != 5*time.Minute || q.WrapUp() != 10*time.Second {
		t.Fatal("durations do not match the configured seconds")
	}
}

func TestAQueueRefusesUnsafeOrAmbiguousSettings(t *testing.T) {
	cases := map[string]struct {
		change func(*Queue)
		want   error
	}{
		"no name":                {func(q *Queue) { q.Name = " " }, ErrQueueNameRequired},
		"no workspace":           {func(q *Queue) { q.WorkspaceID = "" }, ErrWorkspaceRequired},
		"no members":             {func(q *Queue) { q.DepartmentID = "" }, ErrQueueMembersRequired},
		"department and people":  {func(q *Queue) { q.MemberUserIDs = []string{"u1"} }, ErrQueueMembersAmbiguous},
		"unknown strategy":       {func(q *Queue) { q.Strategy = "ring_everyone" }, ErrInvalidStrategy},
		"ring too short":         {func(q *Queue) { q.RingSeconds = 2 }, ErrQueueTimingOutOfRange},
		"wait without a limit":   {func(q *Queue) { q.MaxWaitSeconds = 0 }, ErrQueueTimingOutOfRange},
		"wait over an hour":      {func(q *Queue) { q.MaxWaitSeconds = 3601 }, ErrQueueTimingOutOfRange},
		"negative wrap up":       {func(q *Queue) { q.WrapUpSeconds = -1 }, ErrQueueTimingOutOfRange},
		"two hold music sources": {func(q *Queue) { q.HoldMusic = HoldMusicRef{PresetID: "lofi", MediaID: "m1"} }, ErrHoldMusicAmbiguous},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			q := validQueue()
			q.ApplyDefaults()
			tc.change(&q)
			if err := q.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestExplicitMembersAreDeduplicated(t *testing.T) {
	q := Queue{WorkspaceID: "ws1", Name: "Suporte", MemberUserIDs: []string{"u1", " u2 ", "u1", ""}}
	q.ApplyDefaults()
	if err := q.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(q.MemberUserIDs) != 2 || q.MemberUserIDs[1] != "u2" {
		t.Fatalf("members = %v", q.MemberUserIDs)
	}
}

func TestTransferNotesAreBounded(t *testing.T) {
	long := make([]rune, MaxTransferNotes+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := NormalizeTransferNotes(string(long)); !errors.Is(err, ErrTransferNotesTooLong) {
		t.Fatalf("err = %v", err)
	}
	if notes, err := NormalizeTransferNotes("  quer segunda via do boleto  "); err != nil || notes != "quer segunda via do boleto" {
		t.Fatalf("notes = %q, %v", notes, err)
	}
}

func TestInvalidInputIsToldApartFromMissingThings(t *testing.T) {
	if !IsInvalidInput(fmt.Errorf("%w: %q", ErrInvalidStrategy, "x")) || !IsInvalidInput(ErrQueueMemberOutside) {
		t.Fatal("a rejected queue was not recognised as invalid input")
	}
	if IsInvalidInput(ErrQueueNotFound) || IsInvalidInput(ErrTargetUnavailable) {
		t.Fatal("a missing or busy target was reported as invalid input")
	}
}
