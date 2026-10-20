package cron

import (
	"context"
	"testing"
	"time"
)

func TestMercadoLivreJobsRunOnTheirSchedule(t *testing.T) {
	r := &JobRunner{}
	noop := CtxJobFunc(func(context.Context) error { return nil })
	r.SetMercadoLivreJobs(noop, noop, noop, noop)
	want := map[string]time.Duration{
		"mercadolivre_token_refresh": 30 * time.Minute,
		"mercadolivre_health_check":  24 * time.Hour,
		"mercadolivre_reconcile":     10 * time.Minute,
		"mercadolivre_missed_feeds":  time.Hour,
	}
	if len(r.channelJobs) != len(want) {
		t.Fatalf("jobs = %+v", r.channelJobs)
	}
	for _, j := range r.channelJobs {
		if want[j.name] != j.period {
			t.Errorf("%s every %v", j.name, j.period)
		}
	}
}
