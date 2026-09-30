package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

type fakeHistoryRuns struct {
	mu      sync.Mutex
	runs    map[string]*uw.HistorySync
	seq     int
	resumed []string
	saved   []uw.HistorySync
}

func newFakeHistoryRuns() *fakeHistoryRuns {
	return &fakeHistoryRuns{runs: map[string]*uw.HistorySync{}}
}

func (f *fakeHistoryRuns) Create(_ context.Context, s *uw.HistorySync) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runs {
		if r.InstanceID == s.InstanceID && r.Status.Active() {
			return uw.ErrHistorySyncActive
		}
	}
	f.seq++
	s.ID = "run-" + string(rune('0'+f.seq))
	clone := *s
	f.runs[s.ID] = &clone
	return nil
}

func (f *fakeHistoryRuns) find(instanceID string, activeOnly bool) (*uw.HistorySync, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found *uw.HistorySync
	for _, r := range f.runs {
		if r.InstanceID != instanceID || (activeOnly && !r.Status.Active()) {
			continue
		}
		if found == nil || r.CreatedAt.After(found.CreatedAt) {
			found = r
		}
	}
	if found == nil {
		return nil, uw.ErrHistorySyncNotFound
	}
	clone := *found
	return &clone, nil
}

func (f *fakeHistoryRuns) FindActive(_ context.Context, instanceID string) (*uw.HistorySync, error) {
	return f.find(instanceID, true)
}

func (f *fakeHistoryRuns) FindLatest(_ context.Context, instanceID string) (*uw.HistorySync, error) {
	return f.find(instanceID, false)
}

func (f *fakeHistoryRuns) Resume(_ context.Context, id string, now, pollUntil time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumed = append(f.resumed, id)
	if r, ok := f.runs[id]; ok {
		r.NextPollAt = now
		if pollUntil.After(r.PollUntil) {
			r.PollUntil = pollUntil
		}
	}
	return nil
}

func (f *fakeHistoryRuns) ClaimDue(_ context.Context, now time.Time, owner string, leaseUntil time.Time, limit int) ([]*uw.HistorySync, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*uw.HistorySync
	for _, r := range f.runs {
		if !r.Status.Active() || r.NextPollAt.After(now) || (r.LeaseUntil != nil && r.LeaseUntil.After(now)) {
			continue
		}
		r.Status, r.LeaseOwner, r.LeaseUntil = uw.HistorySyncRunning, owner, &leaseUntil
		if r.StartedAt == nil {
			started := now
			r.StartedAt = &started
		}
		clone := *r
		out = append(out, &clone)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeHistoryRuns) Save(_ context.Context, s *uw.HistorySync) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	current, ok := f.runs[s.ID]
	if !ok || current.LeaseOwner != s.LeaseOwner || s.LeaseOwner == "" {
		return uw.ErrHistorySyncLeaseLost
	}
	clone := *s
	clone.LeaseOwner, clone.LeaseUntil = "", nil
	f.runs[s.ID] = &clone
	f.saved = append(f.saved, clone)
	return nil
}

func (f *fakeHistoryRuns) only(t *testing.T) *uw.HistorySync {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(f.runs))
	}
	for _, r := range f.runs {
		clone := *r
		return &clone
	}
	return nil
}

type fakeHistorySource struct {
	mu    sync.Mutex
	pages [][]map[string]any
	err   error
	calls []uw.FindMessagesInput
}

func (f *fakeHistorySource) FindMessages(_ context.Context, _ uw.InstanceRef, in uw.FindMessagesInput) (*uw.MessagePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	if f.err != nil {
		return nil, f.err
	}
	index := len(f.calls) - 1
	if index >= len(f.pages) {
		return &uw.MessagePage{Messages: json.RawMessage(`[]`)}, nil
	}
	raw, _ := json.Marshal(f.pages[index])
	return &uw.MessagePage{Messages: raw, Returned: len(f.pages[index])}, nil
}

func (f *fakeHistorySource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeLines struct {
	mu        sync.Mutex
	siblings  []*uw.Instance
	err       error
	transfers [][2]string
	events    *[]string
}

func (f *fakeLines) ListSameNumber(context.Context, *uw.Instance) ([]*uw.Instance, error) {
	return f.siblings, f.err
}

func (f *fakeLines) Transfer(_ context.Context, from, to string) (uw.LineTransfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transfers = append(f.transfers, [2]string{from, to})
	if f.events != nil {
		*f.events = append(*f.events, "handover")
	}
	return uw.LineTransfer{Conversations: 1}, nil
}

type orderedSource struct {
	*fakeHistorySource
	events *[]string
}

func (o orderedSource) FindMessages(ctx context.Context, ref uw.InstanceRef, in uw.FindMessagesInput) (*uw.MessagePage, error) {
	*o.events = append(*o.events, "find")
	return o.fakeHistorySource.FindMessages(ctx, ref, in)
}

type syncHarness struct {
	*importHarness
	sync   *HistorySyncUseCase
	runs   *fakeHistoryRuns
	source *fakeHistorySource
	lines  *fakeLines
	clock  time.Time
}

func newSyncHarness(t *testing.T) *syncHarness {
	t.Helper()
	imp := newImportHarness(t)
	h := &syncHarness{
		importHarness: imp,
		runs:          newFakeHistoryRuns(),
		source:        &fakeHistorySource{},
		lines:         &fakeLines{},
		clock:         time.Now().UTC(),
	}
	h.sync = NewHistorySyncUseCase(HistorySyncDeps{
		Runs:      h.runs,
		Instances: newFakeInstanceRepo(imp.instance),
		Servers:   newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		Source:    h.source,
		Importer:  imp.uc,
		Handover:  NewLineHandover(h.lines),
	})
	h.sync.now = func() time.Time { return h.clock }
	h.sync.sleep = func(context.Context, time.Duration) error { return nil }
	h.sync.spawn = func(fn func()) { fn() }
	h.sync.pageSize = 2
	return h
}

func (h *syncHarness) queue(t *testing.T) *uw.HistorySync {
	t.Helper()
	run, err := h.sync.Schedule(context.Background(), h.instance, uw.HistorySyncTriggerConnect)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	return run
}

func (h *syncHarness) tick(t *testing.T) {
	t.Helper()
	if err := h.sync.Execute(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

func recent(h *syncHarness, ago time.Duration) time.Time { return h.clock.Add(-ago) }

func TestScheduleQueuesOneRunAndResumesItOnASecondConnect(t *testing.T) {
	h := newSyncHarness(t)

	first := h.queue(t)
	second := h.queue(t)

	if first.ID != second.ID {
		t.Errorf("a second connect created run %s beside %s", second.ID, first.ID)
	}
	if len(h.runs.resumed) != 1 {
		t.Errorf("resumed = %v, want the active run resumed once", h.runs.resumed)
	}
}

func TestSweepPagesUntilAShortPageAndImports(t *testing.T) {
	h := newSyncHarness(t)
	h.source.pages = [][]map[string]any{
		{historyItem("a1", anaChat, false, recent(h, time.Hour)), historyItem("a2", anaChat, true, recent(h, 2*time.Hour))},
		{historyItem("b1", brunoChat, false, recent(h, 3*time.Hour))},
	}
	h.queue(t)

	h.tick(t)

	if n := h.source.callCount(); n != 2 {
		t.Errorf("pages fetched = %d; a full page must be followed by another, whatever hasMore said", n)
	}
	if h.source.calls[1].Offset != 2 {
		t.Errorf("second page offset = %d, want 2", h.source.calls[1].Offset)
	}
	run := h.runs.only(t)
	if run.MessagesImported != 3 || run.Passes != 1 || run.Status != uw.HistorySyncRunning {
		t.Errorf("run = %+v", run)
	}
	if run.OldestMessageAt == nil {
		t.Error("the reached horizon must be recorded")
	}
	if n := h.assignments.count(); n != 0 {
		t.Errorf("the poller triggered %d assignments", n)
	}
}

func TestSweepStopsPagingAtTheWindow(t *testing.T) {
	h := newSyncHarness(t)
	h.sync.SetWindow(24 * time.Hour)
	h.source.pages = [][]map[string]any{
		{historyItem("new", anaChat, false, recent(h, time.Hour)), historyItem("old", anaChat, false, recent(h, 48*time.Hour))},
		{historyItem("older", anaChat, false, recent(h, 72*time.Hour))},
	}
	h.queue(t)

	h.tick(t)

	if n := h.source.callCount(); n != 1 {
		t.Errorf("pages fetched = %d; the host returns newest first, so an old message ends the pass", n)
	}
}

func TestSweepPausesWhileTheNumberIsDisconnected(t *testing.T) {
	h := newSyncHarness(t)
	h.queue(t)
	h.instance.Status = uw.StatusDisconnected

	h.tick(t)

	if h.source.callCount() != 0 {
		t.Error("a disconnected number must not be read")
	}
	run := h.runs.only(t)
	if run.PausedSince == nil || !run.Status.Active() {
		t.Errorf("run = %+v, want paused and still active", run)
	}
}

func TestSweepCancelsTheRunOfARemovedNumber(t *testing.T) {
	h := newSyncHarness(t)
	h.queue(t)
	h.sync.instances = newFakeInstanceRepo()

	h.tick(t)

	if run := h.runs.only(t); run.Status != uw.HistorySyncCancelled {
		t.Errorf("status = %s, want CANCELLED", run.Status)
	}
}

func TestSweepProviderFailures(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		check func(*testing.T, *uw.HistorySync)
	}{
		{"a restriction stops the import", &uw.ProviderError{HTTPStatus: 400, ProviderCode: 463}, func(t *testing.T, r *uw.HistorySync) {
			if r.Status != uw.HistorySyncPartial {
				t.Errorf("status = %s, want PARTIAL", r.Status)
			}
		}},
		{"a lost session pauses", &uw.ProviderError{HTTPStatus: http.StatusUnauthorized}, func(t *testing.T, r *uw.HistorySync) {
			if r.PausedSince == nil || !r.Status.Active() {
				t.Errorf("run = %+v, want paused", r)
			}
		}},
		{"a host outage retries", &uw.ProviderError{HTTPStatus: http.StatusBadGateway}, func(t *testing.T, r *uw.HistorySync) {
			if r.Attempts != 1 || !r.Status.Active() || r.LastError == "" {
				t.Errorf("run = %+v, want one retryable attempt", r)
			}
		}},
		{"a refused request fails", &uw.ProviderError{HTTPStatus: http.StatusBadRequest}, func(t *testing.T, r *uw.HistorySync) {
			if r.Status != uw.HistorySyncFailed {
				t.Errorf("status = %s, want FAILED", r.Status)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newSyncHarness(t)
			h.source.err = tc.err
			h.queue(t)

			h.tick(t)

			tc.check(t, h.runs.only(t))
		})
	}
}

func TestSweepHandsTheLineOverBeforeImporting(t *testing.T) {
	h := newSyncHarness(t)
	var events []string
	h.lines.events = &events
	h.lines.siblings = []*uw.Instance{{
		ID: "old-inst", WorkspaceID: "ws-1", PhoneNumber: h.instance.PhoneNumber, Status: uw.StatusDisconnected,
	}}
	h.sync.source = orderedSource{fakeHistorySource: h.source, events: &events}
	h.sync.spawn = func(func()) {}
	h.queue(t)

	h.tick(t)

	if len(events) < 2 || events[0] != "handover" || events[1] != "find" {
		t.Errorf("events = %v; old conversations must move before history can create duplicates of them", events)
	}
}

func TestSweepHoldsTheImportWhenTheHandoverFails(t *testing.T) {
	h := newSyncHarness(t)
	h.lines.err = errors.New("db down")
	h.queue(t)

	h.tick(t)

	if h.source.callCount() != 0 {
		t.Error("importing before the handover succeeds would split threads across two instances")
	}
	if run := h.runs.only(t); run.Attempts != 1 || !run.Status.Active() {
		t.Errorf("run = %+v, want a retryable attempt", run)
	}
}

func TestConnectTransitionQueuesTheImportAndHandsOver(t *testing.T) {
	h := newSyncHarness(t)
	h.instance.Status = uw.StatusAwaitingScan
	h.lines.siblings = []*uw.Instance{{
		ID: "old-inst", WorkspaceID: "ws-1", PhoneNumber: h.instance.PhoneNumber, Status: uw.StatusDisconnected,
	}}
	sync := sessionSync{instances: newFakeInstanceRepo(h.instance), listener: h.sync}

	if _, err := sync.apply(context.Background(), h.instance, &uw.Session{State: "connected", Connected: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := sync.apply(context.Background(), h.instance, &uw.Session{State: "connected", Connected: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if run := h.runs.only(t); run.Trigger != uw.HistorySyncTriggerConnect {
		t.Errorf("run = %+v", run)
	}
	if len(h.lines.transfers) != 1 {
		t.Errorf("transfers = %v; only the transition into connected hands the line over", h.lines.transfers)
	}
}

func TestManualRequestIsScopedAndRateLimited(t *testing.T) {
	h := newSyncHarness(t)
	ctx := context.Background()

	if _, err := h.sync.Request(ctx, h.instance.ID, "other-ws", uw.DepartmentScope{}); !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Errorf("cross-workspace request: err = %v, want not found", err)
	}

	run, err := h.sync.Request(ctx, h.instance.ID, "ws-1", uw.DepartmentScope{})
	if err != nil || run.Trigger != uw.HistorySyncTriggerManual {
		t.Fatalf("request: %v, %+v", err, run)
	}

	h.tick(t)
	h.clock = h.clock.Add(3 * time.Hour)
	h.tick(t)
	h.clock = h.clock.Add(time.Minute)
	if _, err := h.sync.Request(ctx, h.instance.ID, "ws-1", uw.DepartmentScope{}); !errors.Is(err, uw.ErrHistorySyncCooldown) {
		t.Errorf("request right after a finished run: err = %v, want cooldown", err)
	}

	h.instance.Status = uw.StatusDisconnected
	h.clock = h.clock.Add(2 * time.Hour)
	if _, err := h.sync.Request(ctx, h.instance.ID, "ws-1", uw.DepartmentScope{}); !errors.Is(err, uw.ErrInstanceNotConnected) {
		t.Errorf("request on a disconnected number: err = %v", err)
	}
}

func TestResetReReadsTheSession(t *testing.T) {
	instances, servers, instance := connectFixture(uw.StatusConnected)
	provider := &fakeProvider{
		StatusFn: func(context.Context, uw.InstanceRef) (*uw.Session, error) {
			return &uw.Session{State: "disconnected"}, nil
		},
	}
	uc := NewConnectInstanceUseCase(instances, servers, provider)

	if err := uc.Reset(context.Background(), instance.ID, "ws-1", uw.DepartmentScope{}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	if instance.Status != uw.StatusDisconnected {
		t.Errorf("status = %s; a reset number must stop showing as connected at once", instance.Status)
	}
}

func TestConnectWithoutHistoryStillHandsTheLineOver(t *testing.T) {
	h := newSyncHarness(t)
	h.instance.ImportHistory = false
	h.lines.siblings = []*uw.Instance{{
		ID: "old-inst", WorkspaceID: "ws-1", PhoneNumber: h.instance.PhoneNumber, Status: uw.StatusDisconnected,
	}}

	h.sync.InstanceConnected(context.Background(), h.instance)

	if len(h.runs.runs) != 0 {
		t.Errorf("runs = %d; a number that opted out must not queue an import", len(h.runs.runs))
	}
	if len(h.lines.transfers) != 1 {
		t.Error("opting out of history must not strand the previous link's conversations")
	}
}

func TestSweepCancelsARunWhenTheNumberOptsOut(t *testing.T) {
	h := newSyncHarness(t)
	h.queue(t)
	h.instance.ImportHistory = false

	h.tick(t)

	if h.source.callCount() != 0 {
		t.Error("the provider was read after the owner switched history off")
	}
	if run := h.runs.only(t); run.Status != uw.HistorySyncCancelled {
		t.Errorf("status = %s, want CANCELLED", run.Status)
	}
}

func TestManualRequestIsRefusedWhenTheNumberOptedOut(t *testing.T) {
	h := newSyncHarness(t)
	h.instance.ImportHistory = false

	_, err := h.sync.Request(context.Background(), h.instance.ID, "ws-1", uw.DepartmentScope{})

	if !errors.Is(err, uw.ErrHistoryImportOff) {
		t.Errorf("err = %v, want ErrHistoryImportOff", err)
	}
}
