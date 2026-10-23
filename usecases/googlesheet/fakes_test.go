package googlesheet

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
)

type memorySheets struct {
	mu     sync.Mutex
	sheets map[string]*googlesheet.Sheet
	fail   error
}

func newMemorySheets() *memorySheets { return &memorySheets{sheets: map[string]*googlesheet.Sheet{}} }

func copySheet(s *googlesheet.Sheet) *googlesheet.Sheet {
	c := *s
	c.Columns = append([]googlesheet.Column(nil), s.Columns...)
	return &c
}

func (m *memorySheets) Create(_ context.Context, s *googlesheet.Sheet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	m.sheets[s.ID] = copySheet(s)
	return nil
}

func (m *memorySheets) Update(_ context.Context, s *googlesheet.Sheet, expected int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.sheets[s.ID]
	if !ok || stored.Version != expected {
		return googlesheet.ErrStaleSheet
	}
	m.sheets[s.ID] = copySheet(s)
	return nil
}

func (m *memorySheets) SaveStatus(_ context.Context, s *googlesheet.Sheet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.sheets[s.ID]
	if !ok {
		return googlesheet.ErrSheetNotFound
	}
	stored.Status, stored.Reason, stored.ReasonDetail = s.Status, s.Reason, s.ReasonDetail
	return nil
}

func (m *memorySheets) UpdateTabName(_ context.Context, id, tab string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sheets[id].TabName = tab
	return nil
}

func (m *memorySheets) Get(_ context.Context, id string) (*googlesheet.Sheet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sheets[id]
	if !ok {
		return nil, googlesheet.ErrSheetNotFound
	}
	return copySheet(s), nil
}

func (m *memorySheets) FindInWorkspace(ctx context.Context, ws, id string) (*googlesheet.Sheet, error) {
	s, err := m.Get(ctx, id)
	if err != nil || s.WorkspaceID != ws {
		return nil, googlesheet.ErrSheetNotFound
	}
	return s, nil
}

func (m *memorySheets) List(context.Context, googlesheet.ListInput) (*shared.PaginatedResult[*googlesheet.Sheet], error) {
	return nil, errors.New("not used")
}

func (m *memorySheets) Delete(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sheets, id)
	return nil
}

func (m *memorySheets) TouchWritten(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sheets[id]; ok {
		s.LastWrittenAt = &at
	}
	return nil
}

type memoryRows struct {
	mu   sync.Mutex
	rows map[string]*googlesheet.Row
	seq  int
}

func newMemoryRows() *memoryRows { return &memoryRows{rows: map[string]*googlesheet.Row{}} }

func (m *memoryRows) Insert(_ context.Context, row *googlesheet.Row) (*googlesheet.Row, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.rows {
		if existing.SheetID == row.SheetID && existing.IdempotencyKey == row.IdempotencyKey {
			c := *existing
			return &c, true, nil
		}
	}
	m.seq++
	row.ID = fmt.Sprintf("row-%03d", m.seq)
	row.CreatedAt = row.CreatedAt.Add(time.Duration(m.seq) * time.Microsecond)
	c := *row
	m.rows[row.ID] = &c
	return row, false, nil
}

func (m *memoryRows) ordered(sheetID string, statuses ...googlesheet.RowStatus) []*googlesheet.Row {
	var out []*googlesheet.Row
	for _, r := range m.rows {
		if r.SheetID != sheetID {
			continue
		}
		for _, s := range statuses {
			if r.Status == s {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *memoryRows) PendingSheets(_ context.Context, readyAt time.Time, limit int) ([]googlesheet.PendingSheet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []googlesheet.PendingSheet
	for _, r := range m.rows {
		if seen[r.SheetID] || r.Status != googlesheet.RowPending {
			continue
		}
		seen[r.SheetID] = true
		head := m.ordered(r.SheetID, googlesheet.RowPending)[0]
		if !head.NextAttemptAt.After(readyAt) {
			out = append(out, googlesheet.PendingSheet{SheetID: r.SheetID, OldestPendingAt: head.CreatedAt})
		}
	}
	return out, nil
}

func (m *memoryRows) Claim(_ context.Context, sheetID, batchID string, limit int, now time.Time) ([]*googlesheet.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pending := m.ordered(sheetID, googlesheet.RowPending)
	if len(pending) == 0 || pending[0].NextAttemptAt.After(now) {
		return nil, nil
	}
	if len(pending) > limit {
		pending = pending[:limit]
	}
	out := make([]*googlesheet.Row, len(pending))
	for i, r := range pending {
		r.Status, r.BatchID, r.Attempts = googlesheet.RowInFlight, batchID, r.Attempts+1
		claimedAt := now
		r.WrittenAt = nil
		r.NextAttemptAt = claimedAt
		c := *r
		out[i] = &c
	}
	return out, nil
}

func (m *memoryRows) set(ids []string, change func(*googlesheet.Row)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		if r, ok := m.rows[id]; ok {
			change(r)
		}
	}
}

func (m *memoryRows) MarkWritten(_ context.Context, rows []googlesheet.WrittenRow, at time.Time) error {
	for _, w := range rows {
		number := w.RowNumber
		m.set([]string{w.ID}, func(r *googlesheet.Row) {
			r.Status, r.RowNumber, r.WrittenAt, r.ErrorCode = googlesheet.RowWritten, number, &at, ""
		})
	}
	return nil
}

func (m *memoryRows) Release(_ context.Context, ids []string, next time.Time, code string) error {
	m.set(ids, func(r *googlesheet.Row) { r.Status, r.NextAttemptAt, r.ErrorCode = googlesheet.RowPending, next, code })
	return nil
}

func (m *memoryRows) Unclaim(_ context.Context, ids []string) error {
	m.set(ids, func(r *googlesheet.Row) {
		r.Status = googlesheet.RowPending
		if r.Attempts > 0 {
			r.Attempts--
		}
	})
	return nil
}

func (m *memoryRows) Hold(_ context.Context, ids []string, code, detail string) error {
	m.set(ids, func(r *googlesheet.Row) { r.Status, r.ErrorCode, r.ErrorDetail = googlesheet.RowHeld, code, detail })
	return nil
}

func (m *memoryRows) Fail(_ context.Context, id, code, detail string) error {
	m.set([]string{id}, func(r *googlesheet.Row) { r.Status, r.ErrorCode, r.ErrorDetail = googlesheet.RowFailed, code, detail })
	return nil
}

func (m *memoryRows) RequeueProblems(_ context.Context, sheetID string, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.ordered(sheetID, googlesheet.RowHeld, googlesheet.RowFailed) {
		r.Status, r.Attempts, r.NextAttemptAt = googlesheet.RowPending, 0, now
		n++
	}
	return n, nil
}

func (m *memoryRows) StuckInFlight(_ context.Context, sheetID string, before time.Time) ([]*googlesheet.Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*googlesheet.Row
	for _, r := range m.ordered(sheetID, googlesheet.RowInFlight) {
		if r.NextAttemptAt.Before(before) {
			c := *r
			out = append(out, &c)
		}
	}
	return out, nil
}

func (m *memoryRows) PurgeWritten(context.Context, time.Time, int) (int, error) { return 0, nil }

func (m *memoryRows) DeleteForLead(context.Context, string, string) (int, error) { return 0, nil }

func (m *memoryRows) DeleteUnwritten(_ context.Context, sheetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, r := range m.rows {
		if r.SheetID == sheetID && r.Status != googlesheet.RowWritten {
			delete(m.rows, id)
		}
	}
	return nil
}

func (m *memoryRows) Health(_ context.Context, sheetID string, _ int) (googlesheet.Health, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := googlesheet.Health{}
	for _, r := range m.rows {
		if r.SheetID != sheetID {
			continue
		}
		switch r.Status {
		case googlesheet.RowPending:
			h.Pending++
		case googlesheet.RowInFlight:
			h.InFlight++
		case googlesheet.RowHeld:
			h.Held++
		case googlesheet.RowFailed:
			h.Failed++
		case googlesheet.RowWritten:
			h.Written++
		}
	}
	return h, nil
}

func (m *memoryRows) byStatus(sheetID string, status googlesheet.RowStatus) []*googlesheet.Row {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ordered(sheetID, status)
}

type appendCall struct {
	refs []string
}

type fakeAPI struct {
	mu         sync.Mutex
	header     []any
	refs       [][]any
	nextRow    int
	appends    []appendCall
	appendErrs []error
	rejectRef  map[string]bool
	tab        googlesheet.TabInfo
	readErr    error
	createErr  error
	created    googlesheet.CreatedSpreadsheet
	trashed    []string
	applied    int
	readCalls  int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{nextRow: 2, tab: googlesheet.TabInfo{Title: "Pedidos", Rows: 1000, Columns: 10}, rejectRef: map[string]bool{}}
}

func (f *fakeAPI) CreateSpreadsheet(context.Context, string, googlesheet.SpreadsheetSpec) (googlesheet.CreatedSpreadsheet, error) {
	return f.created, f.createErr
}

func (f *fakeAPI) TrashSpreadsheet(_ context.Context, _, id string) error {
	f.trashed = append(f.trashed, id)
	return nil
}

func (f *fakeAPI) ApplyColumns(context.Context, string, googlesheet.Target, []googlesheet.Column) error {
	f.applied++
	return nil
}

var errInvalidRow = errors.New("invalid row")

func (f *fakeAPI) AppendRows(_ context.Context, _ string, _ googlesheet.Target, rows [][]any) (googlesheet.AppendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	refs := make([]string, len(rows))
	for i, row := range rows {
		refs[i] = row[0].(string)
	}
	f.appends = append(f.appends, appendCall{refs: refs})
	if len(f.appendErrs) > 0 {
		err := f.appendErrs[0]
		f.appendErrs = f.appendErrs[1:]
		if err != nil {
			return googlesheet.AppendResult{}, err
		}
	}
	for _, ref := range refs {
		if f.rejectRef[ref] {
			return googlesheet.AppendResult{}, errInvalidRow
		}
	}
	first := f.nextRow
	for _, ref := range refs {
		f.refs = append(f.refs, []any{ref})
	}
	f.nextRow += len(rows)
	return googlesheet.AppendResult{FirstRow: first, Rows: len(rows)}, nil
}

func (f *fakeAPI) ReadRange(_ context.Context, _ string, _ googlesheet.Target, a1 string) ([][]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls++
	if f.readErr != nil {
		return nil, f.readErr
	}
	if a1 == "1:1" {
		return [][]any{f.header}, nil
	}
	return append([][]any{{"ref"}}, f.refs...), nil
}

func (f *fakeAPI) TabInfo(context.Context, string, googlesheet.Target) (googlesheet.TabInfo, error) {
	return f.tab, f.readErr
}

func (f *fakeAPI) written() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, row := range f.refs {
		out = append(out, row[0].(string))
	}
	return out
}

var (
	errRateLimited = errors.New("429")
	errTransient   = errors.New("503")
	errForbidden   = errors.New("403")
	errAuth        = errors.New("401")
)

type fakeClassifier struct{}

func (fakeClassifier) Classify(err error) googlesheet.Failure {
	switch {
	case errors.Is(err, errRateLimited):
		return googlesheet.Failure{Kind: googlesheet.FailureRateLimited, RetryAfter: 30 * time.Second}
	case errors.Is(err, errForbidden):
		return googlesheet.Failure{Kind: googlesheet.FailurePermission, Detail: "no access"}
	case errors.Is(err, errAuth):
		return googlesheet.Failure{Kind: googlesheet.FailureAuth}
	case errors.Is(err, errInvalidRow):
		return googlesheet.Failure{Kind: googlesheet.FailureInvalid, Detail: "bad value"}
	}
	return googlesheet.Failure{Kind: googlesheet.FailureTransient, Detail: "unavailable"}
}

type fakeAccess struct {
	access googleaccount.Access
	err    error
}

func (f *fakeAccess) Access(context.Context, string, googleaccount.Product) (googleaccount.Access, error) {
	return f.access, f.err
}

type fakeLimiter struct {
	mu       sync.Mutex
	writes   int
	reads    int
	pressure float64
	err      error
}

func (l *fakeLimiter) TakeWrite(context.Context, string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return false, l.err
	}
	if l.writes == 0 {
		return false, nil
	}
	l.writes--
	return true, nil
}

func (l *fakeLimiter) TakeRead(context.Context, string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return false, l.err
	}
	if l.reads == 0 {
		return false, nil
	}
	l.reads--
	return true, nil
}

func (l *fakeLimiter) WritePressure(context.Context) (float64, error) { return l.pressure, nil }

type fakeLeases struct {
	mu   sync.Mutex
	held map[string]bool
}

func (l *fakeLeases) Acquire(ctx context.Context, key string) (func() (bool, error), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	l.held[key] = true
	return func() (bool, error) {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.held, key)
		return true, nil
	}, nil
}

type fakeMarks struct {
	mu   sync.Mutex
	keys map[string]bool
}

func (m *fakeMarks) SetNX(key, _ string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys[key] {
		return false, nil
	}
	m.keys[key] = true
	return true, nil
}

func (m *fakeMarks) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.keys, k)
	}
	return nil
}

type fakeAlerts struct {
	mu        sync.Mutex
	attention []googlesheet.AttentionReason
	resolved  int
}

func (a *fakeAlerts) NeedsAttention(_ context.Context, s *googlesheet.Sheet) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attention = append(a.attention, s.Reason)
	return nil
}

func (a *fakeAlerts) Resolved(context.Context, *googlesheet.Sheet) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resolved++
	return nil
}

type fakeMetrics struct{}

func (fakeMetrics) AddGoogleSheetsEvent(string, string)   {}
func (fakeMetrics) ObserveGoogleSheetsBatch(int, float64) {}

type recordingNudger struct {
	mu     sync.Mutex
	sheets []string
}

func (n *recordingNudger) Nudge(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sheets = append(n.sheets, id)
}

type fakeResolver struct {
	values googlesheet.SystemValues
	calls  int
}

func (r *fakeResolver) Resolve(context.Context, string, googlesheet.Origin) (googlesheet.SystemValues, error) {
	r.calls++
	return r.values, nil
}
