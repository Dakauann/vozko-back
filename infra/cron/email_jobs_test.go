package cron

import (
	"context"
	"testing"
	"time"
)

func TestTheEmailTenantRetirementSweepRunsDaily(t *testing.T) {
	r := &JobRunner{}
	r.SetEmailTenantRetirementJob(CtxJobFunc(func(context.Context) error { return nil }))
	if len(r.channelJobs) != 1 || r.channelJobs[0].name != "email_tenant_retirement_sweep" || r.channelJobs[0].period != 24*time.Hour {
		t.Fatalf("jobs = %+v", r.channelJobs)
	}
}

func TestTheUnusedEmailAccountSweepRunsDaily(t *testing.T) {
	r := &JobRunner{}
	r.SetEmailUnusedReleaseJob(CtxJobFunc(func(context.Context) error { return nil }))
	if len(r.channelJobs) != 1 || r.channelJobs[0].name != "email_unused_account_release" || r.channelJobs[0].period != 24*time.Hour {
		t.Fatalf("jobs = %+v", r.channelJobs)
	}
}
