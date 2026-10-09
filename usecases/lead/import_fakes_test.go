package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"vozko/domain/lead"
	"vozko/domain/leadimport"
	"vozko/domain/unofficial_whatsapp"
)

type fakeImportStore struct {
	mu         sync.Mutex
	jobs       map[string]*leadimport.Job
	issues     map[string][]leadimport.IssueRow
	links      map[string][]leadimport.PendingLink
	seq        int64
	saves      int
	placements map[string]leadimport.Placement
	placed     []string
	issuesErr  error
}

func newFakeImportStore() *fakeImportStore {
	return &fakeImportStore{jobs: map[string]*leadimport.Job{}, issues: map[string][]leadimport.IssueRow{}, links: map[string][]leadimport.PendingLink{}}
}

func copyJob(j *leadimport.Job) *leadimport.Job {
	c := *j
	if j.Settings != nil {
		s := *j.Settings
		c.Settings = &s
	}
	if j.DryRun != nil {
		d := j.DryRun.Clone()
		c.DryRun = &d
	}
	if j.Result != nil {
		r := j.Result.Clone()
		c.Result = &r
	}
	if j.Seed != nil {
		seed := *j.Seed
		c.Seed = &seed
	}
	return &c
}

func (s *fakeImportStore) activeIn(workspaceID, except string) bool {
	for id, j := range s.jobs {
		if id != except && j.WorkspaceID == workspaceID && j.Status.Active() {
			return true
		}
	}
	return false
}

func (s *fakeImportStore) Create(_ context.Context, j *leadimport.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.Status.Active() && s.activeIn(j.WorkspaceID, j.ID) {
		return leadimport.ErrRunning
	}
	s.jobs[j.ID] = copyJob(j)
	return nil
}

func (s *fakeImportStore) Get(_ context.Context, workspaceID, id string) (*leadimport.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok || j.WorkspaceID != workspaceID {
		return nil, leadimport.ErrNotFound
	}
	return copyJob(j), nil
}

func (s *fakeImportStore) Save(ctx context.Context, j *leadimport.Job, guard leadimport.Guard) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.jobs[j.ID]
	if !ok {
		return leadimport.ErrNotFound
	}
	if !slices.Contains(guard.From, stored.Status) {
		return leadimport.ErrNotReady
	}
	if guard.Claim != "" && stored.Claim != guard.Claim {
		return leadimport.ErrClaimLost
	}
	if j.Status.Active() && s.activeIn(j.WorkspaceID, j.ID) {
		return leadimport.ErrRunning
	}
	s.saves++
	s.jobs[j.ID] = copyJob(j)
	return nil
}

func (s *fakeImportStore) Claim(_ context.Context, id, token string, now time.Time) (*leadimport.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, leadimport.ErrNotFound
	}
	if !j.Claimable(now) {
		return nil, leadimport.ErrClaimLost
	}
	j.Claim, j.HeartbeatAt, j.Attempts = token, &now, j.Attempts+1
	return copyJob(j), nil
}

func (s *fakeImportStore) Claimable(_ context.Context, now time.Time, limit int) ([]leadimport.Ref, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var refs []leadimport.Ref
	for id, j := range s.jobs {
		if j.Claimable(now) {
			refs = append(refs, leadimport.Ref{ID: id, WorkspaceID: j.WorkspaceID})
		}
	}
	slices.SortFunc(refs, func(a, b leadimport.Ref) int { return strings.Compare(a.ID, b.ID) })
	return refs[:min(limit, len(refs))], nil
}

func (s *fakeImportStore) FailStalled(_ context.Context, now time.Time) ([]leadimport.Stalled, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stalled []leadimport.Stalled
	for _, j := range s.jobs {
		if j.Status.Active() && j.Attempts >= leadimport.MaxAttempts && (j.HeartbeatAt == nil || j.HeartbeatAt.Before(now.Add(-leadimport.StaleAfter))) {
			stalled = append(stalled, leadimport.Stalled{Ref: leadimport.Ref{ID: j.ID, WorkspaceID: j.WorkspaceID}, Status: j.Status})
			j.Status, j.FailureCode, j.Claim = leadimport.StatusFailed, leadimport.FailureStalled, ""
		}
	}
	return stalled, nil
}

func (s *fakeImportStore) Expired(_ context.Context, now time.Time, limit int) ([]leadimport.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []leadimport.Job
	for _, j := range s.jobs {
		if j.Expired(now) && !j.Status.Active() && len(out) < limit {
			out = append(out, *copyJob(j))
		}
	}
	return out, nil
}

func (s *fakeImportStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	delete(s.issues, id)
	delete(s.links, id)
	return nil
}

func (s *fakeImportStore) Issues(_ context.Context, importID string, after int64, limit int) ([]leadimport.IssueRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.issuesErr != nil {
		return nil, s.issuesErr
	}
	var out []leadimport.IssueRow
	for _, row := range s.issues[importID] {
		if row.Seq > after && len(out) < limit {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *fakeImportStore) Unused(_ context.Context, workspaceID, requestedBy string) ([]leadimport.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []leadimport.Job
	for _, j := range s.jobs {
		if j.WorkspaceID == workspaceID && j.RequestedBy == requestedBy && j.Unused() {
			out = append(out, *copyJob(j))
		}
	}
	return out, nil
}

func (s *fakeImportStore) job(id string) *leadimport.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyJob(s.jobs[id])
}

func (s *fakeImportStore) checkpoint(job *leadimport.Job, apply func(stored *leadimport.Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.jobs[job.ID]
	if !ok || stored.Claim != job.Claim {
		return leadimport.ErrClaimLost
	}
	apply(stored)
	return nil
}

func (s *fakeImportStore) record(importID string, issues []lead.ImportIssue) {
	for _, issue := range issues {
		s.seq++
		s.issues[importID] = append(s.issues[importID], leadimport.IssueRow{Seq: s.seq, ImportIssue: issue})
	}
}

type fakeLeadBase struct {
	mu        sync.Mutex
	leads     map[string]*lead.Lead
	relations []lead.Relation
	next      int
	writes    int
	written   []string
	failRows  bool
	holderCap int
	loaded    [][]string
}

func newFakeLeadBase(leads ...*lead.Lead) *fakeLeadBase {
	b := &fakeLeadBase{leads: map[string]*lead.Lead{}}
	for _, l := range leads {
		l.EnsureCollections()
		b.leads[l.ID] = l
	}
	return b
}

func (b *fakeLeadBase) copyOf(l *lead.Lead) *lead.Lead {
	c := *l
	c.Phones = slices.Clone(l.Phones)
	c.Addresses = slices.Clone(l.Addresses)
	c.EnsureCollections()
	return &c
}

func (b *fakeLeadBase) ByIdentity(_ context.Context, workspaceID string, numbers []string) (map[string]*lead.Lead, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]*lead.Lead{}
	for _, l := range b.leads {
		if l.WorkspaceID != workspaceID || !l.HasIdentity() {
			continue
		}
		for _, n := range numbers {
			if l.HoldsIdentity(n) {
				for _, format := range lead.NumberFormats(l.Number) {
					out[format] = b.copyOf(l)
				}
			}
		}
	}
	return out, nil
}

func (b *fakeLeadBase) HoldingPhones(_ context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.loaded = append(b.loaded, numbers)
	var out []*lead.Lead
	for _, l := range b.leads {
		if l.WorkspaceID != workspaceID {
			continue
		}
		for _, n := range numbers {
			if l.HoldsNumber(n) {
				out = append(out, b.copyOf(l))
				break
			}
		}
	}
	if b.holderCap > 0 && len(out) > b.holderCap {
		return nil, leadimport.ErrTooManyHolders
	}
	return out, nil
}

func (b *fakeLeadBase) IdentityIDs(ctx context.Context, workspaceID string, numbers []string) (map[string]string, error) {
	found, _ := b.ByIdentity(ctx, workspaceID, numbers)
	out := map[string]string{}
	for format, l := range found {
		out[format] = l.ID
	}
	return out, nil
}

func (b *fakeLeadBase) byNumber(number string) *lead.Lead {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range b.leads {
		if l.HoldsIdentity(number) {
			return l
		}
	}
	return nil
}

func (b *fakeLeadBase) byName(name string) *lead.Lead {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range b.leads {
		if l.Name == name {
			return l
		}
	}
	return nil
}

type fakeImportWriter struct {
	base   *fakeLeadBase
	store  *fakeImportStore
	rows   [][]int
	calls  int
	failAt int
}

func (w *fakeImportWriter) WriteRows(ctx context.Context, batch leadimport.RowBatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.calls++
	if w.base.failRows || w.calls == w.failAt {
		return errors.New("database down")
	}
	w.base.mu.Lock()
	outcomes := make([]leadimport.RowOutcome, 0, len(batch.Items))
	var lines []int
	for _, item := range batch.Items {
		lines = append(lines, item.Record.Line)
		d, err := batch.Decide(item.Existing, item.Record)
		if err != nil {
			w.base.mu.Unlock()
			return err
		}
		o := leadimport.RowOutcome{Record: item.Record, Decision: d, Matched: item.Existing}
		switch d.Verdict {
		case lead.ImportCreated:
			w.base.next++
			d.Lead.ID = fmt.Sprintf("lead-new-%d", w.base.next)
			w.base.leads[d.Lead.ID] = d.Lead
			o.LeadID = d.Lead.ID
			w.base.written = append(w.base.written, d.Lead.ID)
		case lead.ImportEnriched:
			d.Lead.Version++
			w.base.leads[d.Lead.ID] = d.Lead
			o.LeadID = d.Lead.ID
			w.base.written = append(w.base.written, d.Lead.ID)
		case lead.ImportUnchanged, lead.ImportSkipped:
			o.LeadID = item.Existing.ID
		}
		outcomes = append(outcomes, o)
	}
	w.base.writes++
	w.base.mu.Unlock()
	w.rows = append(w.rows, lines)
	cp := batch.Checkpoint(outcomes)
	return w.store.checkpoint(batch.Job, func(stored *leadimport.Job) {
		result := cp.Result.Clone()
		stored.Processed, stored.Result = cp.Processed, &result
		w.store.record(batch.Job.ID, cp.Issues)
		for idx, l := range cp.Links {
			l.ID = fmt.Sprintf("%s-link-%d-%d", batch.Job.ID, cp.Processed, idx)
			w.store.links[batch.Job.ID] = append(w.store.links[batch.Job.ID], l)
		}
	})
}

func (w *fakeImportWriter) PendingLinks(_ context.Context, importID string, limit int) ([]leadimport.PendingLink, error) {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	links := w.store.links[importID]
	return slices.Clone(links[:min(limit, len(links))]), nil
}

func (w *fakeImportWriter) WriteLinks(_ context.Context, batch leadimport.LinkBatch) error {
	created := 0
	var failed []lead.ImportIssue
	w.base.mu.Lock()
	for _, l := range batch.Links {
		if l.Issue != nil {
			failed = append(failed, *l.Issue)
			continue
		}
		w.base.relations = append(w.base.relations, *l.Relation)
		created++
	}
	w.base.mu.Unlock()
	result := batch.Checkpoint(created, failed)
	return w.store.checkpoint(batch.Job, func(stored *leadimport.Job) {
		stored.Result = &result
		w.store.record(batch.Job.ID, failed)
		done := map[string]bool{}
		for _, l := range batch.Links {
			done[l.Link.ID] = true
		}
		var kept []leadimport.PendingLink
		for _, l := range w.store.links[batch.Job.ID] {
			if !done[l.ID] {
				kept = append(kept, l)
			}
		}
		w.store.links[batch.Job.ID] = kept
	})
}

type fakeImportFiles struct {
	files     map[string][]byte
	hidden    map[string]bool
	erased    []string
	next      int
	failReads int
}

func (f *fakeImportFiles) Store(_ context.Context, workspaceID, name string, data []byte) (string, error) {
	f.next++
	id := fmt.Sprintf("media-%d", f.next)
	f.files[workspaceID+"/"+id] = data
	if f.hidden == nil {
		f.hidden = map[string]bool{}
	}
	f.hidden[id] = true
	return id, nil
}

func (f *fakeImportFiles) ReadLibrary(ctx context.Context, workspaceID, mediaID string) ([]byte, error) {
	if f.hidden[mediaID] {
		return nil, leadimport.ErrFileUnavailable
	}
	return f.Read(ctx, workspaceID, mediaID)
}

func (f *fakeImportFiles) Read(_ context.Context, workspaceID, mediaID string) ([]byte, error) {
	if f.failReads > 0 {
		f.failReads--
		return nil, errors.New("storage timeout")
	}
	data, ok := f.files[workspaceID+"/"+mediaID]
	if !ok {
		return nil, leadimport.ErrFileUnavailable
	}
	return data, nil
}

func (f *fakeImportFiles) Erase(_ context.Context, workspaceID, mediaID string) error {
	delete(f.files, workspaceID+"/"+mediaID)
	f.erased = append(f.erased, mediaID)
	return nil
}

type fakeMembers map[string]string

func (m fakeMembers) UserIDsByEmail(_ context.Context, _ string, emails []string) (map[string]string, error) {
	out := map[string]string{}
	for _, e := range emails {
		if id, ok := m[strings.ToLower(e)]; ok {
			out[strings.ToLower(e)] = id
		}
	}
	return out, nil
}

type fakeSeeder struct {
	published []unofficial_whatsapp.SeedRequest
	err       error
	failAt    int
	onPublish func()
}

func (s *fakeSeeder) Publish(in unofficial_whatsapp.SeedRequest) (unofficial_whatsapp.SeedQueued, error) {
	if s.onPublish != nil {
		s.onPublish()
	}
	s.published = append(s.published, in)
	if s.err != nil || len(s.published) == s.failAt {
		if s.err == nil {
			return unofficial_whatsapp.SeedQueued{}, errors.New("broker down")
		}
		return unofficial_whatsapp.SeedQueued{}, s.err
	}
	queued := unofficial_whatsapp.SeedQueued{Targets: len(in.Targets)}
	if in.Script != nil {
		queued.Scripted = len(in.Targets)
	}
	return queued, nil
}

type queuedRuns struct{ runs []func() }

func (q *queuedRuns) run(fn func()) { q.runs = append(q.runs, fn) }

func (q *queuedRuns) drain() {
	for len(q.runs) > 0 {
		next := q.runs[0]
		q.runs = q.runs[1:]
		next()
	}
}

func (b *fakeLeadBase) PhoneHolderCounts(_ context.Context, workspaceID string, numbers []string) (map[string]int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	counts := map[string]int{}
	for _, number := range numbers {
		for _, format := range lead.NumberFormats(number) {
			if _, counted := counts[format]; counted {
				continue
			}
			held := 0
			for _, l := range b.leads {
				if l.WorkspaceID == workspaceID && l.HoldsNumber(format) {
					held++
				}
			}
			if b.holderCap > 0 && held > b.holderCap {
				held = leadimport.MaxContactHolders + 1
			}
			counts[format] = held
		}
	}
	return counts, nil
}

func (s *fakeImportStore) Mine(_ context.Context, workspaceID, requestedBy string, _ time.Time, limit int) ([]leadimport.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []leadimport.Job
	for _, j := range s.jobs {
		if j.WorkspaceID == workspaceID && len(out) < limit {
			out = append(out, *copyJob(j))
		}
	}
	return out, nil
}

func (s *fakeImportStore) Placement(_ context.Context, workspaceID, importID string) (leadimport.Placement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.placed = append(s.placed, workspaceID+"/"+importID)
	return s.placements[importID], nil
}

type fakeImportMetrics struct {
	mu        sync.Mutex
	rows      map[string]int
	runs      map[string]int
	durations []time.Duration
}

func newFakeImportMetrics() *fakeImportMetrics {
	return &fakeImportMetrics{rows: map[string]int{}, runs: map[string]int{}}
}

func (m *fakeImportMetrics) AddLeadImportRows(outcome string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[outcome] += n
}

func (m *fakeImportMetrics) AddLeadImportRuns(outcome string, n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[outcome] += n
}

func (m *fakeImportMetrics) ObserveLeadImportDuration(elapsed time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.durations = append(m.durations, elapsed)
}
