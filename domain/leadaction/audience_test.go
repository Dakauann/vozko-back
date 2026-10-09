package leadaction

import (
	"testing"
	"time"

	"vozko/domain/advertising"
)

func TestAnAudienceJobLeftPendingTooLongReadsAsStalled(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	job := AudienceJob{ID: "a", ActorID: "u", Status: AudiencePending, StartedAt: at, UpdatedAt: at}
	if got := job.Effective(at.Add(time.Minute)); got.Status != AudiencePending {
		t.Fatalf("a fresh job = %+v", got)
	}
	if got := job.Effective(at.Add(AudienceStaleAfter + time.Second)); got.Status != AudienceFailed || got.FailureCode != string(FailureStalled) {
		t.Fatalf("a stalled job = %+v", got)
	}
	job.Succeed(advertising.Audience{MetaID: "m"}, 3, 1, at)
	if got := job.Effective(at.Add(48 * time.Hour)); got.Status != AudienceDone || got.Audience.MetaID != "m" || got.Matched != 3 {
		t.Fatalf("a finished job = %+v", got)
	}
	job.Fail("", at)
	if job.FailureCode != string(FailureInternal) {
		t.Fatalf("a failure without a code = %+v", job)
	}
	if job.VisibleTo("other") || !job.VisibleTo("u") || job.VisibleTo("") {
		t.Fatal("an audience job is read by its actor only")
	}
}
