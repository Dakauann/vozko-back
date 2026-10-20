package cron

import (
	"context"
	"testing"
	"time"

	ttdomain "vozko/domain/tiktok"
)

func TestTikTokJobsRunOnTheirSchedule(t *testing.T) {
	r := &JobRunner{}
	noop := CtxJobFunc(func(context.Context) error { return nil })
	r.SetTikTokJobs(noop, noop, noop, noop)
	want := map[string]time.Duration{
		"tiktok_token_refresh": time.Hour,
		"tiktok_health_check":  24 * time.Hour,
		"tiktok_purge":         24 * time.Hour,
		"tiktok_comment_work":  10 * time.Second,
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

func TestTheTokenRefreshRunsWellInsideTheRefreshLead(t *testing.T) {
	if ttdomain.TokenRefreshLead < 3*time.Hour {
		t.Fatalf("the hourly refresh needs a lead of several hours, got %v", ttdomain.TokenRefreshLead)
	}
}
