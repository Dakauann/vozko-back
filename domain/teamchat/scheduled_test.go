package teamchat

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestValidateSendAt(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want error
	}{
		{"in an hour", created.Add(time.Hour), nil},
		{"exactly the lead", created.Add(shared.MessageScheduleLead), nil},
		{"too soon", created.Add(30 * time.Second), ErrSendAtTooSoon},
		{"in the past", created.Add(-time.Hour), ErrSendAtTooSoon},
		{"exactly the horizon", created.Add(shared.MessageScheduleHorizon), nil},
		{"past the horizon", created.Add(shared.MessageScheduleHorizon + time.Second), ErrSendAtTooFar},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSendAt(tc.at, created); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func scheduled(t *testing.T) Scheduled {
	t.Helper()
	s, err := NewScheduled("s1", thread, "ana", Draft{Body: " lembrete ", Mentions: []string{"bia"}}, created.Add(time.Hour), created)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewScheduled(t *testing.T) {
	s := scheduled(t)
	if s.Status != ScheduledPending || s.Body != "lembrete" || s.ThreadID != "t1" || s.WorkspaceID != "ws" || s.AuthorID != "ana" {
		t.Fatalf("scheduled = %+v", s)
	}
	if _, err := NewScheduled("s2", thread, "ana", Draft{Body: ""}, created.Add(time.Hour), created); !errors.Is(err, ErrBodyRequired) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := NewScheduled("s2", thread, "ana", Draft{Body: "x"}, created, created); !errors.Is(err, ErrSendAtTooSoon) {
		t.Fatalf("now: %v", err)
	}
}

func TestADeliveredScheduledMessageUsesAStableClientID(t *testing.T) {
	s := scheduled(t)
	draft := s.Draft()
	if draft.ClientID != "scheduled:s1" || draft.Body != "lembrete" || len(draft.Mentions) != 1 {
		t.Fatalf("draft = %+v", draft)
	}
}

func TestRescheduleBy(t *testing.T) {
	s := scheduled(t)
	later := created.Add(10 * time.Minute)
	body := " novo texto "
	at := created.Add(2 * time.Hour)
	changed, err := s.RescheduleBy("ana", &body, &at, later)
	if err != nil || changed.Body != "novo texto" || !changed.SendAt.Equal(at) || !changed.UpdatedAt.Equal(later) {
		t.Fatalf("changed = %+v, err = %v", changed, err)
	}
	if _, err := s.RescheduleBy("bia", &body, nil, later); !errors.Is(err, ErrNotAuthor) {
		t.Fatalf("someone else: %v", err)
	}
	soon := later
	if _, err := s.RescheduleBy("ana", nil, &soon, later); !errors.Is(err, ErrSendAtTooSoon) {
		t.Fatalf("too soon: %v", err)
	}
	s.Status = ScheduledSent
	if _, err := s.RescheduleBy("ana", &body, nil, later); !errors.Is(err, ErrScheduledFinal) {
		t.Fatalf("already sent: %v", err)
	}
}

func TestScheduledTransitions(t *testing.T) {
	s := scheduled(t)
	canceled, err := s.CancelBy("ana", created)
	if err != nil || canceled.Status != ScheduledCanceled {
		t.Fatalf("canceled = %+v, err = %v", canceled, err)
	}
	if _, err := s.CancelBy("bia", created); !errors.Is(err, ErrNotAuthor) {
		t.Fatalf("someone else: %v", err)
	}
	if _, err := canceled.CancelBy("ana", created); !errors.Is(err, ErrScheduledFinal) {
		t.Fatalf("twice: %v", err)
	}
	sent, err := s.Sent("m9", created)
	if err != nil || sent.Status != ScheduledSent || sent.SentMessageID != "m9" {
		t.Fatalf("sent = %+v, err = %v", sent, err)
	}
	failed, err := s.Failed(ReasonRecipientUnavailable, created)
	if err != nil || failed.Status != ScheduledFailed || failed.FailureReason != ReasonRecipientUnavailable {
		t.Fatalf("failed = %+v, err = %v", failed, err)
	}
	if _, err := sent.Failed(ReasonNotMember, created); !errors.Is(err, ErrScheduledFinal) {
		t.Fatalf("sent then failed: %v", err)
	}
}

func TestDue(t *testing.T) {
	s := scheduled(t)
	if s.Due(created) {
		t.Fatal("not due before its time")
	}
	if !s.Due(s.SendAt) {
		t.Fatal("due at its time")
	}
	s.Status = ScheduledCanceled
	if s.Due(s.SendAt.Add(time.Hour)) {
		t.Fatal("a canceled message is never due")
	}
}
