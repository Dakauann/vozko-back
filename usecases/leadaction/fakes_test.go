package leadaction_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/cache"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	"vozko/domain/selection"
	adsuc "vozko/usecases/advertising"
	report_usecase "vozko/usecases/report"
)

const (
	workspaceID = "ws-1"
	manager     = "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"
	seller      = "9f8e7d6c-5b4a-4321-8765-0fedcba98765"
	phoneID     = "3a2b1c0d-9e8f-4a7b-8c6d-5e4f3a2b1c0d"
)

var start = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

type fakeRuns struct {
	mu       sync.Mutex
	runs     map[string]*leadaction.Run
	saves    int
	create   int
	hideKeys int
}

func newFakeRuns() *fakeRuns { return &fakeRuns{runs: map[string]*leadaction.Run{}} }

func (f *fakeRuns) Create(_ context.Context, r *leadaction.Run) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.create++
	for _, existing := range f.runs {
		if existing.WorkspaceID == r.WorkspaceID && existing.IdempotencyKey == r.IdempotencyKey {
			return leadaction.ErrRunExists
		}
	}
	copied := *r
	f.runs[r.ID] = &copied
	return nil
}

func (f *fakeRuns) FindByKey(_ context.Context, ws, key string) (*leadaction.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hideKeys > 0 {
		f.hideKeys--
		return nil, leadaction.ErrRunNotFound
	}
	for _, r := range f.runs {
		if r.WorkspaceID == ws && r.IdempotencyKey == key {
			copied := *r
			return &copied, nil
		}
	}
	return nil, leadaction.ErrRunNotFound
}

func (f *fakeRuns) Get(_ context.Context, ws, id string) (*leadaction.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok || r.WorkspaceID != ws {
		return nil, leadaction.ErrRunNotFound
	}
	copied := *r
	return &copied, nil
}

func (f *fakeRuns) Claim(_ context.Context, id, token string, now time.Time) (*leadaction.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return nil, leadaction.ErrRunNotFound
	}
	if err := r.Start(token, now); err != nil {
		return nil, err
	}
	copied := *r
	return &copied, nil
}

func (f *fakeRuns) Save(_ context.Context, r *leadaction.Run, claim string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.runs[r.ID]
	if !ok || stored.Claim != claim || claim == "" {
		return leadaction.ErrClaimLost
	}
	f.saves++
	copied := *r
	copied.Result.Skipped = map[leadaction.SkipReason]int{}
	for k, v := range r.Result.Skipped {
		copied.Result.Skipped[k] = v
	}
	f.runs[r.ID] = &copied
	return nil
}

func (f *fakeRuns) Claimable(_ context.Context, now time.Time, _ int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id, r := range f.runs {
		if r.Claimable(now) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (f *fakeRuns) FailStalled(_ context.Context, now time.Time) (map[leadaction.Action]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stalled := map[leadaction.Action]int{}
	for _, r := range f.runs {
		if r.Status == leadaction.StatusRunning && r.Attempts >= leadaction.MaxAttempts && r.HeartbeatAt != nil && r.HeartbeatAt.Before(now.Add(-leadaction.StaleAfter)) {
			r.Fail(leadaction.FailureStalled, now)
			stalled[r.Action]++
		}
	}
	return stalled, nil
}

func (f *fakeRuns) only() *leadaction.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runs {
		copied := *r
		return &copied
	}
	return nil
}

type fakeSelections struct {
	selectedCount int
	matched       int
	pages         [][]string
	snapshots     map[string][]string
	freezes       int
	counted       []selection.Selection
	resolved      []string
	dropped       []string
	frozenSel     []selection.Selection
	frozenIDs     []string
	pendings      []*lead.Assignment
	selected      []selection.Selection
	paged         []string
}

func newFakeSelections(matched int, ids []string) *fakeSelections {
	return &fakeSelections{matched: matched, frozenIDs: ids, snapshots: map[string][]string{}}
}

func (f *fakeSelections) Count(_ context.Context, _ selection.Scope, s selection.Selection) (int, error) {
	f.counted = append(f.counted, s)
	return f.matched, nil
}

func (f *fakeSelections) Resolve(_ context.Context, _ selection.Scope, _ selection.Selection, after string, limit int) ([]selection.Ref, error) {
	f.resolved = append(f.resolved, after)
	return refsAfter(f.frozenIDs, after, limit), nil
}

func refsAfter(ids []string, after string, limit int) []selection.Ref {
	var out []selection.Ref
	for _, id := range ids {
		if after != "" && id <= after {
			continue
		}
		out = append(out, selection.Ref{ID: id, Type: lead.SelectionRefType})
		if len(out) == limit {
			break
		}
	}
	return out
}

func (f *fakeSelections) Selected(_ context.Context, _ selection.Scope, s selection.Selection) (int, error) {
	f.selected = append(f.selected, s)
	if f.selectedCount > 0 {
		return f.selectedCount, nil
	}
	return len(f.frozenIDs), nil
}

func (f *fakeSelections) Freeze(_ context.Context, _ selection.Scope, s selection.Selection, snapshotID string, pending *lead.Assignment) (lead.Frozen, error) {
	if existing, ok := f.snapshots[snapshotID]; ok {
		return lead.Frozen{Size: len(existing), Existed: true}, nil
	}
	f.freezes++
	f.frozenSel = append(f.frozenSel, s)
	f.pendings = append(f.pendings, pending)
	f.snapshots[snapshotID] = append([]string{}, f.frozenIDs...)
	return lead.Frozen{Size: len(f.frozenIDs)}, nil
}

func (f *fakeSelections) SnapshotSize(_ context.Context, _, snapshotID string) (int, error) {
	return len(f.snapshots[snapshotID]), nil
}

func (f *fakeSelections) Snapshot(_ context.Context, _, snapshotID, after string, limit int) ([]string, error) {
	f.paged = append(f.paged, snapshotID)
	var out []string
	for _, ref := range refsAfter(f.snapshots[snapshotID], after, limit) {
		out = append(out, ref.ID)
	}
	return out, nil
}

func (f *fakeSelections) DropSnapshot(_ context.Context, _, snapshotID string) error {
	f.dropped = append(f.dropped, snapshotID)
	delete(f.snapshots, snapshotID)
	return nil
}

func (f *fakeSelections) SweepSnapshots(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

type leadState struct {
	blocked bool
	owner   string
	values  map[string]any
	version int64
	number  string
}

type fakeWriter struct {
	mu      sync.Mutex
	leads   map[string]*leadState
	batches int
	sizes   []int
	events  int
}

func newFakeWriter(ids []string) *fakeWriter {
	w := &fakeWriter{leads: map[string]*leadState{}}
	for i, id := range ids {
		w.leads[id] = &leadState{values: map[string]any{}, version: 1, number: fmt.Sprintf("55319%08d", i)}
	}
	return w
}

func (w *fakeWriter) holds(s *leadState, e leadaction.Edit) bool {
	switch e.Kind {
	case leadaction.EditBlocked:
		return s.blocked == e.Blocks()
	case leadaction.EditOwner:
		return s.owner == e.Owner()
	default:
		current, ok := s.values[e.Key]
		if e.Value == nil {
			return !ok
		}
		return ok && fmt.Sprint(current) == fmt.Sprint(e.Value)
	}
}

func (w *fakeWriter) ApplyBatch(_ context.Context, b leadaction.BatchWrite) (leadaction.Tally, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.batches++
	w.sizes = append(w.sizes, len(b.LeadIDs))
	var t leadaction.Tally
	for _, id := range b.LeadIDs {
		s, ok := w.leads[id]
		if !ok {
			t.Gone++
			continue
		}
		if w.holds(s, b.Edit) {
			t.Unchanged++
			continue
		}
		switch b.Edit.Kind {
		case leadaction.EditBlocked:
			s.blocked = b.Edit.Blocks()
		case leadaction.EditOwner:
			s.owner = b.Edit.Owner()
		default:
			s.values[b.Edit.Key] = b.Edit.Value
		}
		s.version++
		w.events++
		t.Changed = append(t.Changed, leadaction.Changed{LeadID: id, Version: s.version})
	}
	return t, nil
}

func (w *fakeWriter) TallyBatch(_ context.Context, _ string, ids []string, e leadaction.Edit) (leadaction.Tally, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var t leadaction.Tally
	for _, id := range ids {
		s, ok := w.leads[id]
		if !ok {
			t.Gone++
			continue
		}
		if w.holds(s, e) {
			t.Unchanged++
		}
	}
	return t, nil
}

func (w *fakeWriter) BlockTargets(_ context.Context, _ string, ids []string, blocked bool) ([]leadaction.BlockTarget, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []leadaction.BlockTarget
	for _, id := range ids {
		if s, ok := w.leads[id]; ok && s.blocked == blocked {
			out = append(out, leadaction.BlockTarget{LeadID: id, Number: s.number})
		}
	}
	return out, nil
}

type fakeNotifier struct{ runs []string }

func (n *fakeNotifier) LeadsBulkUpdated(_, runID string) { n.runs = append(n.runs, runID) }

type fakePermissions map[string]map[string]bool

func (p fakePermissions) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return p[userID][resource+":"+action]
}

func managerOnly() fakePermissions {
	return fakePermissions{manager: {
		"leads:read": true, "leads:update": true, "leads:bulk_update": true, "leads:block": true, "leads:assign": true,
		"members:read": true, "leads:read_sensitive": true, "leads:read_addresses": true, "leads:export": true,
		"reports:create": true, "reports:read": true, "ads:create": true, "call_lists:read": true, "call_lists:manage": true,
	}, seller: {"leads:read": true}}
}

type fakeDefinitions []*customfield.Definition

func (d fakeDefinitions) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return d, nil
}

func definitions() fakeDefinitions {
	return fakeDefinitions{
		{Key: "interesse", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"alto", "baixo"}},
		{Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect, Options: []string{"Positivo", "Negativo"}, Sensitive: true, LegalBasis: "consentimento"},
	}
}

type fakeOwners struct{ refused map[string]error }

func (o fakeOwners) CheckOwner(_ Actor, owner string) error { return o.refused[owner] }

type fakeMetaPhone struct {
	applied []string
	fail    map[string]bool
}

func (p *fakeMetaPhone) Apply(number string, _ bool) error {
	if p.fail[number] {
		return errors.New("meta refused")
	}
	p.applied = append(p.applied, number)
	return nil
}

type fakeMetaPhones struct {
	phone *fakeMetaPhone
	err   error
}

func (f *fakeMetaPhones) Phone(string, string) (lead.WhatsAppBlock, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.phone, nil
}

type fakeLimiter struct {
	allow  int
	refill int
	retry  time.Duration
	keys   []string
}

func (l *fakeLimiter) Allow(key string) (bool, time.Duration, error) {
	l.keys = append(l.keys, key)
	if l.allow <= 0 {
		return false, l.retry, nil
	}
	l.allow--
	return true, 0, nil
}

type fakeState struct {
	cache.SharedState
	mu sync.Mutex
	m  map[string]string
}

func newFakeState() *fakeState { return &fakeState{m: map[string]string{}} }

func (s *fakeState) SetString(key, value string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

func (s *fakeState) GetString(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key], nil
}

func (s *fakeState) SetNX(key, value string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false, nil
	}
	s.m[key] = value
	return true, nil
}

func (s *fakeState) Del(keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.m, k)
	}
	return nil
}

type fakeGate struct{ acquired int }

func (g *fakeGate) Acquire(context.Context) (func(), error) {
	g.acquired++
	return func() {}, nil
}

type fakeReports struct {
	inputs     []report_usecase.CreateInput
	jobs       []*report.Job
	now        func() time.Time
	failNextBy error
}

func (r *fakeReports) Create(in report_usecase.CreateInput) (*report.Job, error) {
	r.inputs = append(r.inputs, in)
	if err := r.failNextBy; err != nil {
		r.failNextBy = nil
		return nil, err
	}
	for _, job := range r.jobs {
		if job.WorkspaceID == in.WorkspaceID && job.Kind == in.Kind && string(job.Params) == string(in.Params) {
			return job, nil
		}
	}
	job := &report.Job{ID: fmt.Sprintf("job-%d", len(r.jobs)+1), WorkspaceID: in.WorkspaceID, Kind: in.Kind, Params: in.Params, Status: report.StatusQueued}
	if r.now != nil {
		job.CreatedAt = r.now()
	}
	r.jobs = append(r.jobs, job)
	return job, nil
}

type fakeAudiences struct {
	drafts     []advertising.CustomerListDraft
	requesters []adsuc.Requester
	skipped    int
	err        error
}

func (a *fakeAudiences) CreateCustomerList(_ context.Context, r adsuc.Requester, d advertising.CustomerListDraft) (*adsuc.CustomerListResult, error) {
	a.drafts = append(a.drafts, d)
	a.requesters = append(a.requesters, r)
	if a.err != nil {
		return nil, a.err
	}
	return &adsuc.CustomerListResult{Audience: advertising.Audience{MetaID: "aud-1", Name: d.Name}, Matched: 2, Skipped: a.skipped}, nil
}

type fakeMetrics struct {
	mu        sync.Mutex
	runs      map[string]int
	durations map[string][]time.Duration
	skips     map[string]int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{runs: map[string]int{}, durations: map[string][]time.Duration{}, skips: map[string]int{}}
}

func (m *fakeMetrics) AddLeadActionRuns(action, status, failure string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n > 0 {
		m.runs[action+"|"+status+"|"+failure] += n
	}
}

func (m *fakeMetrics) ObserveLeadActionDuration(action, status string, elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations[action+"|"+status] = append(m.durations[action+"|"+status], elapsed)
}

func (m *fakeMetrics) AddLeadActionSkips(action, reason string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n > 0 {
		m.skips[action+"|"+reason] += n
	}
}

type harness struct {
	svc        *Service
	runs       *fakeRuns
	selections *fakeSelections
	writer     *fakeWriter
	notifier   *fakeNotifier
	perms      fakePermissions
	phone      *fakeMetaPhone
	limiter    *fakeLimiter
	state      *fakeState
	gate       *fakeGate
	reports    *fakeReports
	audiences  *fakeAudiences
	callLists  *fakeCallLists
	sends      *fakeSends
	metrics    *fakeMetrics
	clock      *clock
	queued     []func()
	owners     fakeOwners
	metaPhones *fakeMetaPhones
	slept      []time.Duration
	scheduled  []scheduledRun
}

type scheduledRun struct {
	after time.Duration
	run   func()
}

func leadIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
	}
	return ids
}

func newHarness(n int) *harness {
	ids := leadIDs(n)
	h := &harness{
		runs: newFakeRuns(), selections: newFakeSelections(n, ids), writer: newFakeWriter(ids), notifier: &fakeNotifier{},
		perms: managerOnly(), phone: &fakeMetaPhone{fail: map[string]bool{}}, limiter: &fakeLimiter{allow: 1 << 30, retry: 30 * time.Second},
		state: newFakeState(), gate: &fakeGate{}, reports: &fakeReports{}, audiences: &fakeAudiences{}, callLists: newFakeCallLists(), sends: &fakeSends{}, metrics: newFakeMetrics(), clock: &clock{at: start},
		owners: fakeOwners{refused: map[string]error{}},
	}
	h.metaPhones = &fakeMetaPhones{phone: h.phone}
	h.reports.now = h.clock.now
	svc, err := NewService(Deps{
		Runs: h.runs, Writer: h.writer, Notifier: h.notifier, Selections: h.selections, Permissions: h.perms,
		Definitions: definitions(), Owners: h.owners, MetaPhones: h.metaPhones, MetaLimiter: h.limiter,
		State: h.state, Gate: h.gate, Reports: h.reports, Audiences: h.audiences, CallLists: h.callLists, Sends: h.sends, Metrics: h.metrics,
		Now: h.clock.now, Background: func(run func()) { h.queued = append(h.queued, run) },
		Sleep: func(_ context.Context, d time.Duration) error {
			h.slept = append(h.slept, d)
			h.limiter.allow = h.limiter.refill
			return nil
		},
		After: func(d time.Duration, run func()) { h.scheduled = append(h.scheduled, scheduledRun{after: d, run: run}) },
	})
	if err != nil {
		panic(err)
	}
	h.svc = svc
	return h
}

func (h *harness) drain() {
	for len(h.queued) > 0 {
		next := h.queued[0]
		h.queued = h.queued[1:]
		next()
	}
}
