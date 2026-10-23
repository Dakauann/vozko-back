package cron

import (
	"context"
	"testing"
	"time"
)

func TestCarrierJobsRunOnTheirSchedule(t *testing.T) {
	r := &JobRunner{}
	noop := CtxJobFunc(func(context.Context) error { return nil })
	r.SetCarrierJobs(noop, noop, noop)
	want := map[string]time.Duration{
		"carrier_message_expiry":     time.Minute,
		"carrier_sender_status_sync": 15 * time.Minute,
		"carrier_campaign_schedules": time.Minute,
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
