package cron

import (
	"context"
	"testing"
	"time"

	workflow_domain "vozko/domain/workflow"
)

func TestWorkflowTraceRetentionRunsEverySixHours(t *testing.T) {
	r := &JobRunner{}
	r.SetWorkflowTraceRetentionJob(CtxJobFunc(func(context.Context) error { return nil }))
	if len(r.channelJobs) != 1 || r.channelJobs[0].name != "workflow_trace_retention" || r.channelJobs[0].period != 6*time.Hour {
		t.Fatalf("jobs = %+v", r.channelJobs)
	}
}

func TestWorkflowRuntimeAlertsAreEvaluatedEveryTwoMinutes(t *testing.T) {
	r := &JobRunner{}
	r.SetWorkflowRuntimeAlertJob(CtxJobFunc(func(context.Context) error { return nil }))
	if len(r.channelJobs) != 1 || r.channelJobs[0].name != "workflow_runtime_alerts" || r.channelJobs[0].period != workflow_domain.RuntimeAlertEvaluationPeriod {
		t.Fatalf("jobs = %+v", r.channelJobs)
	}
}

func TestLostAgentContinuationsAreReconciledEveryFiveMinutes(t *testing.T) {
	r := &JobRunner{}
	r.SetAgentContinuationSweepJob(CtxJobFunc(func(context.Context) error { return nil }))
	if len(r.channelJobs) != 1 || r.channelJobs[0].name != "workflow_agent_continuation_sweep" || r.channelJobs[0].period != workflow_domain.ContinuationSweepPeriod {
		t.Fatalf("jobs = %+v", r.channelJobs)
	}
}
