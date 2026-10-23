package workflow_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	aa "vozko/domain/ai_attendance"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type batchCall struct {
	name   string
	before time.Time
	limit  int
}

type retentionStore struct {
	calls        []batchCall
	remaining    map[string]int64
	failOn       string
	keptVersions int
}

func (s *retentionStore) take(name string, before time.Time, limit int) (int64, error) {
	s.calls = append(s.calls, batchCall{name, before, limit})
	if s.failOn == name {
		return 0, errors.New("db down")
	}
	left := s.remaining[name]
	n := min(left, int64(limit))
	s.remaining[name] = left - n
	return n, nil
}

func (s *retentionStore) PruneStepPayloads(before time.Time, limit int) (int64, error) {
	return s.take("payloads", before, limit)
}

func (s *retentionStore) PruneTriggerData(before time.Time, limit int) (int64, error) {
	return s.take("trigger_data", before, limit)
}

func (s *retentionStore) DeleteStepsOfFinishedRuns(before time.Time, limit int) (int64, error) {
	return s.take("steps", before, limit)
}

func (s *retentionStore) DeleteFinishedRuns(before time.Time, limit int) (int64, error) {
	return s.take("runs", before, limit)
}

func (s *retentionStore) PruneVersions(keepLatest, limit int) (int64, error) {
	s.keptVersions = keepLatest
	return s.take("versions", time.Time{}, limit)
}

func (s *retentionStore) DeleteResolvedBefore(before time.Time, limit int) (int64, error) {
	return s.take("runtime_alerts", before, limit)
}

func (s *retentionStore) PrunePrompts(before time.Time, limit int) (int64, error) {
	return s.take("choice_prompts", before, limit)
}

func (s *retentionStore) PruneCounts(before time.Time, limit int) (int64, error) {
	return s.take("choice_counts", before, limit)
}

type retentionDeliveries struct {
	fakeDeliveryLog
	store *retentionStore
}

func (d *retentionDeliveries) DeleteReceivedBefore(before time.Time, limit int) (int64, error) {
	return d.store.take("deliveries", before, limit)
}

type retentionToolCalls struct {
	store *retentionStore
}

func (r *retentionToolCalls) Record([]aa.ToolCall) error { return nil }

func (r *retentionToolCalls) ListByEntry(string, string, string, shared.Pagination) (*shared.PaginatedResult[*aa.ToolCall], error) {
	return nil, nil
}

func (r *retentionToolCalls) DeleteCreatedBefore(before time.Time, limit int) (int64, error) {
	return r.store.take("tool_calls", before, limit)
}

func retentionJob(store *retentionStore, now time.Time) *TraceRetention {
	job := NewTraceRetention(store, &retentionDeliveries{store: store}, &retentionToolCalls{store: store}, store, store)
	job.now = func() time.Time { return now }
	return job
}

func TestRetentionKeepsThirtyDaysOfPayloadsAndHalfAYearOfRuns(t *testing.T) {
	now := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)
	store := &retentionStore{remaining: map[string]int64{}}
	if err := retentionJob(store, now).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Time{
		"payloads":       now.AddDate(0, 0, -30),
		"trigger_data":   now.AddDate(0, 0, -30),
		"steps":          now.AddDate(0, 0, -180),
		"runs":           now.AddDate(0, 0, -180),
		"deliveries":     now.AddDate(0, 0, -30),
		"tool_calls":     now.AddDate(0, 0, -180),
		"runtime_alerts": now.AddDate(0, 0, -30),
		"versions":       {},
		"choice_prompts": now.AddDate(0, 0, -30),
		"choice_counts":  now.AddDate(0, 0, -400),
	}
	order := []string{}
	for _, c := range store.calls {
		order = append(order, c.name)
		if !c.before.Equal(want[c.name]) || c.limit != retentionBatchSize {
			t.Errorf("%s cut at %v limit %d, want %v limit %d", c.name, c.before, c.limit, want[c.name], retentionBatchSize)
		}
	}
	if strings.Join(order, ",") != "payloads,trigger_data,steps,runs,deliveries,tool_calls,runtime_alerts,versions,choice_prompts,choice_counts" {
		t.Fatalf("order = %v, steps must go before their runs", order)
	}
	if store.keptVersions != workflow.VersionsKeptPerWorkflow {
		t.Fatalf("versions kept = %d, want the latest %d", store.keptVersions, workflow.VersionsKeptPerWorkflow)
	}
}

func TestRetentionLoopsInBoundedBatchesUntilABatchComesBackShort(t *testing.T) {
	store := &retentionStore{remaining: map[string]int64{"steps": 2*int64(retentionBatchSize) + 7}}
	if err := retentionJob(store, time.Now()).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	steps := 0
	for _, c := range store.calls {
		if c.name == "steps" {
			steps++
		}
	}
	if steps != 3 {
		t.Fatalf("step batches = %d, want 3 (full, full, short)", steps)
	}
}

func TestRetentionStopsAtItsBatchBoundAndResumesOnTheNextRun(t *testing.T) {
	store := &retentionStore{remaining: map[string]int64{"payloads": int64(retentionBatchSize) * int64(retentionMaxBatches+50)}}
	if err := retentionJob(store, time.Now()).Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	payloads := 0
	for _, c := range store.calls {
		if c.name == "payloads" {
			payloads++
		}
	}
	if payloads != retentionMaxBatches {
		t.Fatalf("payload batches = %d, want the bound %d", payloads, retentionMaxBatches)
	}
}

func TestOneFailingTableDoesNotStopTheOthers(t *testing.T) {
	store := &retentionStore{remaining: map[string]int64{}, failOn: "steps"}
	err := retentionJob(store, time.Now()).Execute(context.Background())
	if err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v", err)
	}
	seen := map[string]bool{}
	for _, c := range store.calls {
		seen[c.name] = true
	}
	if seen["runs"] {
		t.Fatal("runs must not be deleted while their steps could not be")
	}
	if !seen["deliveries"] || !seen["tool_calls"] {
		t.Fatalf("other tables were skipped: %v", seen)
	}
}

func TestRetentionStopsWhenTheJobIsCancelled(t *testing.T) {
	store := &retentionStore{remaining: map[string]int64{"payloads": 10 * int64(retentionBatchSize)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := retentionJob(store, time.Now()).Execute(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("calls = %+v", store.calls)
	}
}

var _ workflow.TraceRetentionRepository = (*retentionStore)(nil)
