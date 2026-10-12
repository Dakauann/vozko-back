package mediagen_usecase

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/mediagen"
)

func TestABusyProviderFailsTheJobAsBusyWithoutBillingOrResending(t *testing.T) {
	f := newFixture(t)
	f.gen.err = fmt.Errorf("%w: %w", mediagen.ErrGenerationFailed, &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", Limit: "openrouter_in_flight_budget", RetryAfter: 2 * time.Minute, Err: errors.New("status 402")})
	job := f.requestAndProcess(t)
	if job.Status != mediagen.StatusFailed || job.FailureCode != mediagen.FailureProviderBusy || len(f.bill.events) != 0 {
		t.Fatalf("job %+v billed %+v", job, f.bill.events)
	}
	if f.gen.calls != 1 {
		t.Fatalf("a refused generation must not be sent again, sent %d times", f.gen.calls)
	}
	if !strings.Contains(job.FailureDetail, "in_flight_budget_exhausted") {
		t.Fatalf("the detail must keep the provider's reason, got %q", job.FailureDetail)
	}
}

func TestABusyErrorInsideAStartedGenerationStillWaitsForItsCost(t *testing.T) {
	f := newFixture(t)
	f.audio.err = mediagen.Charged("gen-7", &ai.ProviderBusyError{Reason: "in_flight_budget_exhausted", Limit: "openrouter_in_flight_budget"})
	job := f.jobs.job(musicJob(t, f))
	if job.Status != mediagen.StatusSettling || job.FailureCode != mediagen.FailureGeneration {
		t.Fatalf("a generation the provider started is reconciled like any charged failure, got %+v", job)
	}
}
