package googlesheet

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/googleaccount"
	"vozko/domain/googlesheet"
	"vozko/domain/shared"
)

const testWorkspace = "ws-1"

func TestWrittenRowsRemainReadableWithTheirCellsAndAreWorkspaceScoped(t *testing.T) {
	w := newWorld(t)
	queued := w.enqueue(t, "visible-row", map[string]any{"nome": "Dakauann", "valor": 12.5})
	w.write(t)
	page, err := w.manager.ListRows(context.Background(), testWorkspace, w.sheet.ID, shared.Pagination{Page: 1, PageSize: 25})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != queued.RowID || page.Items[0].Status != googlesheet.RowWritten || page.Items[0].Cells["nome"] != "Dakauann" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := w.manager.ListRows(context.Background(), "another-workspace", w.sheet.ID, shared.Pagination{}); !errors.Is(err, googlesheet.ErrSheetNotFound) {
		t.Fatalf("cross-workspace rows exposed: %v", err)
	}
}

type world struct {
	sheets   *memorySheets
	rows     *memoryRows
	api      *fakeAPI
	access   *fakeAccess
	limiter  *fakeLimiter
	leases   *fakeLeases
	marks    *fakeMarks
	alerts   *fakeAlerts
	nudger   *recordingNudger
	resolver *fakeResolver
	writer   *Writer
	intake   *Intake
	manager  *Manager
	now      time.Time
	sheet    *googlesheet.Sheet
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		sheets: newMemorySheets(), rows: newMemoryRows(), api: newFakeAPI(),
		access:   &fakeAccess{access: googleaccount.Access{AccountID: "acct-1", Email: "ops@example.com", Token: "token"}},
		limiter:  &fakeLimiter{writes: 100, reads: 100},
		leases:   &fakeLeases{held: map[string]bool{}},
		marks:    &fakeMarks{keys: map[string]bool{}},
		alerts:   &fakeAlerts{},
		nudger:   &recordingNudger{},
		resolver: &fakeResolver{values: googlesheet.SystemValues{googlesheet.SourceLeadPhone: "+5511999990000"}},
		now:      time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC),
	}
	sheet, err := googlesheet.NewSheet(googlesheet.NewSheetInput{
		WorkspaceID: testWorkspace, AccountID: "acct-1", Title: "Pedidos", TabName: "Pedidos",
		Columns: []googlesheet.ColumnInput{
			{Header: "Nome", Type: googlesheet.TypeText, Required: true},
			{Header: "Valor", Type: googlesheet.TypeMoney},
			{Header: "Telefone", Type: googlesheet.TypePhone, Source: googlesheet.SourceLeadPhone},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sheet.SpreadsheetID = "sp-1"
	if err := w.sheets.Create(context.Background(), sheet); err != nil {
		t.Fatal(err)
	}
	w.sheet = sheet
	w.api.header = toAny(sheet.HeaderRow())
	cfg := DefaultWriterConfig(2)
	cfg.BatchRows = 3
	w.writer = NewWriter(WriterDeps{
		Sheets: w.sheets, Rows: w.rows, API: w.api, Errors: fakeClassifier{}, Access: w.access, Limiter: w.limiter,
		Leases: w.leases, Marks: w.marks, Alerts: w.alerts, Metrics: fakeMetrics{}, Config: cfg,
	})
	w.writer.now = func() time.Time { return w.now }
	w.writer.jitter = func() time.Duration { return 0 }
	w.intake = NewIntake(w.sheets, w.rows, w.resolver, w.nudger)
	w.intake.now = func() time.Time { return w.now }
	w.manager = NewManager(ManagerDeps{
		Sheets: w.sheets, Rows: w.rows, API: w.api, Errors: fakeClassifier{}, Access: w.access, Limiter: w.limiter,
		Marks: w.marks, Alerts: w.alerts, Nudger: w.nudger,
	})
	w.manager.now = func() time.Time { return w.now }
	return w
}

func toAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

func (w *world) enqueue(t *testing.T, key string, values map[string]any) *EnqueueResult {
	t.Helper()
	res, err := w.intake.Enqueue(context.Background(), EnqueueInput{
		WorkspaceID: testWorkspace, SheetID: w.sheet.ID, Values: values, IdempotencyKey: key,
		Origin: googlesheet.Origin{Kind: googlesheet.OriginWorkflow, WorkflowID: "wf-1", RunID: "run-1"},
	})
	if err != nil {
		t.Fatalf("Enqueue %s: %v", key, err)
	}
	w.now = w.now.Add(time.Millisecond)
	return res
}

func (w *world) write(t *testing.T) {
	t.Helper()
	if err := w.writer.WriteSheet(context.Background(), w.sheet.ID); err != nil {
		t.Fatalf("WriteSheet: %v", err)
	}
}

func (w *world) stored(t *testing.T) *googlesheet.Sheet {
	t.Helper()
	s, err := w.sheets.Get(context.Background(), w.sheet.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnqueueValidatesStoresOnceAndNudges(t *testing.T) {
	w := newWorld(t)
	first := w.enqueue(t, "k1", map[string]any{"nome": "Ana", "valor": "R$ 10,50"})
	if first.Duplicate || first.Cells["valor"] != 10.5 || first.Cells["telefone"] != "+5511999990000" {
		t.Fatalf("first = %+v", first)
	}
	again := w.enqueue(t, "k1", map[string]any{"nome": "Ana", "valor": "R$ 10,50"})
	if !again.Duplicate || again.RowID != first.RowID {
		t.Fatalf("retry = %+v", again)
	}
	if len(w.nudger.sheets) != 1 {
		t.Fatalf("nudges = %v; a duplicate must not schedule another write", w.nudger.sheets)
	}
	_, err := w.intake.Enqueue(context.Background(), EnqueueInput{
		WorkspaceID: testWorkspace, SheetID: w.sheet.ID, Values: map[string]any{"valor": 3}, IdempotencyKey: "k2",
		Origin: googlesheet.Origin{Kind: googlesheet.OriginAgent},
	})
	if !errors.Is(err, googlesheet.ErrRequiredColumnMissing) {
		t.Fatalf("missing required err = %v", err)
	}
	if len(w.rows.rows) != 1 {
		t.Fatalf("a refused row was stored: %d rows", len(w.rows.rows))
	}
}

func TestEnqueueRefusesAnotherWorkspacesSheet(t *testing.T) {
	w := newWorld(t)
	_, err := w.intake.Enqueue(context.Background(), EnqueueInput{
		WorkspaceID: "other", SheetID: w.sheet.ID, Values: map[string]any{"nome": "a"}, IdempotencyKey: "k",
		Origin: googlesheet.Origin{Kind: googlesheet.OriginAgent},
	})
	if !errors.Is(err, googlesheet.ErrSheetNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteSheetAppendsInOrderInBatches(t *testing.T) {
	w := newWorld(t)
	var ids []string
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		ids = append(ids, w.enqueue(t, "k"+name, map[string]any{"nome": name, "valor": i}).RowID)
	}
	w.write(t)
	if got := w.api.written(); len(got) != 5 || got[0] != ids[0] || got[4] != ids[4] {
		t.Fatalf("written = %v, want %v", got, ids)
	}
	if len(w.api.appends) != 2 || len(w.api.appends[0].refs) != 3 {
		t.Fatalf("appends = %+v, want two batches of at most 3", w.api.appends)
	}
	written := w.rows.byStatus(w.sheet.ID, googlesheet.RowWritten)
	if len(written) != 5 || written[0].RowNumber != 2 || written[4].RowNumber != 6 {
		t.Fatalf("row numbers = %v", written)
	}
	if w.stored(t).LastWrittenAt == nil {
		t.Fatal("last written not recorded")
	}
}

func TestWriteSheetStopsWhenTheLimiterSaysWait(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.limiter.writes = 0
	w.write(t)
	if len(w.api.appends) != 0 || len(w.rows.byStatus(w.sheet.ID, googlesheet.RowPending)) != 1 {
		t.Fatal("a row was written past the quota or lost")
	}
}

func TestWriteSheetRefusesToRunWithoutItsStore(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.limiter.err = errors.New("redis down")
	if err := w.writer.WriteSheet(context.Background(), w.sheet.ID); err == nil {
		t.Fatal("the writer ignored a limiter without its store")
	}
	if len(w.api.appends) != 0 {
		t.Fatal("rows were written without a quota check")
	}
}

func TestRateLimitBacksOffTheWholeSheet(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.enqueue(t, "k2", map[string]any{"nome": "b"})
	w.api.appendErrs = []error{errRateLimited}
	w.write(t)
	pending := w.rows.byStatus(w.sheet.ID, googlesheet.RowPending)
	if len(pending) != 2 || pending[0].ErrorCode != googlesheet.CodeWriteRateLimited || !pending[0].NextAttemptAt.Equal(w.now.Add(30*time.Second)) {
		t.Fatalf("pending = %+v", pending)
	}
	w.enqueue(t, "k3", map[string]any{"nome": "c"})
	w.write(t)
	if len(w.api.written()) != 0 {
		t.Fatal("a newer row jumped ahead of rows waiting for Google")
	}
	w.now = w.now.Add(time.Minute)
	w.write(t)
	if got := w.api.written(); len(got) != 3 {
		t.Fatalf("written after the wait = %v", got)
	}
}

func TestTransientFailuresHoldAfterTheLastAttempt(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	for i := 0; i < googlesheet.MaxTransientAttempts; i++ {
		w.api.appendErrs = []error{errTransient}
		w.write(t)
		w.now = w.now.Add(2 * googlesheet.MaxBackoff)
	}
	held := w.rows.byStatus(w.sheet.ID, googlesheet.RowHeld)
	if len(held) != 1 {
		t.Fatalf("held = %d, want the row held after %d attempts", len(held), googlesheet.MaxTransientAttempts)
	}
	if s := w.stored(t); s.Status != googlesheet.StatusNeedsAttention || s.Reason != googlesheet.ReasonGoogleUnavailable {
		t.Fatalf("sheet = %+v", s)
	}
}

func TestPermissionLossHoldsRowsAndAlerts(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.api.appendErrs = []error{errForbidden}
	w.write(t)
	if len(w.rows.byStatus(w.sheet.ID, googlesheet.RowHeld)) != 1 {
		t.Fatal("row not held")
	}
	if s := w.stored(t); s.Reason != googlesheet.ReasonAccessLost || len(w.alerts.attention) != 1 {
		t.Fatalf("sheet = %+v, alerts = %v", s, w.alerts.attention)
	}
	if _, err := w.intake.Enqueue(context.Background(), EnqueueInput{
		WorkspaceID: testWorkspace, SheetID: w.sheet.ID, Values: map[string]any{"nome": "b"}, IdempotencyKey: "k2",
		Origin: googlesheet.Origin{Kind: googlesheet.OriginAgent},
	}); !errors.Is(err, googlesheet.ErrSheetNotActive) {
		t.Fatalf("a sheet that needs attention accepted a row: %v", err)
	}
}

func TestAnInvalidRowIsIsolatedAndTheRestKeepsOrder(t *testing.T) {
	w := newWorld(t)
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		ids = append(ids, w.enqueue(t, "k"+name, map[string]any{"nome": name}).RowID)
	}
	w.api.rejectRef[ids[1]] = true
	w.write(t)
	if got := w.api.written(); len(got) != 2 || got[0] != ids[0] || got[1] != ids[2] {
		t.Fatalf("written = %v, want %v and %v in order", got, ids[0], ids[2])
	}
	failed := w.rows.byStatus(w.sheet.ID, googlesheet.RowFailed)
	if len(failed) != 1 || failed[0].ID != ids[1] || failed[0].ErrorCode != googlesheet.CodeWriteRefusedByGoogle {
		t.Fatalf("failed = %+v", failed)
	}
	if w.stored(t).Status != googlesheet.StatusActive {
		t.Fatal("one bad row stopped the whole sheet")
	}
}

func TestHeaderChangedInGoogleStopsWritesBeforeAnyAppend(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.api.header = toAny([]string{"ref", "Nome", "Preço", "Telefone"})
	w.write(t)
	if len(w.api.appends) != 0 {
		t.Fatal("rows were appended under a changed header")
	}
	if s := w.stored(t); s.Reason != googlesheet.ReasonHeaderMismatch {
		t.Fatalf("sheet = %+v", s)
	}
	if len(w.rows.byStatus(w.sheet.ID, googlesheet.RowPending)) != 1 {
		t.Fatal("the row was not kept")
	}
}

func TestHeaderIsCheckedOncePerWindow(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.write(t)
	reads := w.api.readCalls
	w.enqueue(t, "k2", map[string]any{"nome": "b"})
	w.write(t)
	if w.api.readCalls != reads {
		t.Fatalf("header read again inside the window: %d reads, then %d", reads, w.api.readCalls)
	}
}

func TestAccountSwitchedAwayFromTheCreatorNeedsAttention(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.access.access.AccountID = "acct-2"
	w.write(t)
	if s := w.stored(t); s.Reason != googlesheet.ReasonAccountChanged || len(w.api.appends) != 0 {
		t.Fatalf("sheet = %+v, appends %d", s, len(w.api.appends))
	}
}

func TestDisconnectedAccountKeepsRowsWaiting(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.access.err = googleaccount.ErrNeedsReconnect
	w.write(t)
	if len(w.api.appends) != 0 || len(w.rows.byStatus(w.sheet.ID, googlesheet.RowPending)) != 1 || w.stored(t).Status != googlesheet.StatusActive {
		t.Fatal("a lost account dropped or wrote rows, or blamed the sheet")
	}
}

func TestRecoveryFindsRowsGoogleAlreadyHas(t *testing.T) {
	w := newWorld(t)
	a := w.enqueue(t, "ka", map[string]any{"nome": "a"}).RowID
	b := w.enqueue(t, "kb", map[string]any{"nome": "b"}).RowID
	if _, err := w.rows.Claim(context.Background(), w.sheet.ID, "lost-batch", 10, w.now); err != nil {
		t.Fatal(err)
	}
	w.api.refs = append(w.api.refs, []any{a})
	w.api.nextRow = 3
	w.now = w.now.Add(3 * time.Minute)
	w.write(t)
	written := w.rows.byStatus(w.sheet.ID, googlesheet.RowWritten)
	if len(written) != 2 {
		t.Fatalf("written = %d, want both rows", len(written))
	}
	if got := w.api.written(); len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("google rows = %v; the row Google already had must not be sent twice", got)
	}
}

func TestBusyLeaseLeavesTheSheetToItsHolder(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.leases.held["gs:sheet:"+w.sheet.ID] = true
	w.write(t)
	if len(w.api.appends) != 0 {
		t.Fatal("two writers appended to the same sheet")
	}
}

func TestDeletedSheetDropsItsUnwrittenRows(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	delete(w.sheets.sheets, w.sheet.ID)
	w.write(t)
	if len(w.rows.rows) != 0 {
		t.Fatal("rows of a deleted sheet were kept")
	}
}

func TestDispatchWaitsForTheLinger(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	if err := w.writer.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.api.appends) != 0 {
		t.Fatal("a fresh row was sent before the linger")
	}
	w.now = w.now.Add(3 * time.Second)
	if err := w.writer.DispatchPending(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.api.written()) != 1 {
		t.Fatal("the row was not sent after the linger")
	}
}

func TestCreateTrashesTheSpreadsheetWhenItCannotBeRecorded(t *testing.T) {
	w := newWorld(t)
	w.api.created = googlesheet.CreatedSpreadsheet{SpreadsheetID: "sp-new", TabID: 7}
	w.sheets.fail = errors.New("db down")
	_, err := w.manager.Create(context.Background(), CreateInput{
		WorkspaceID: testWorkspace, Title: "Leads", Columns: []googlesheet.ColumnInput{{Header: "Nome", Type: googlesheet.TypeText}},
	})
	if err == nil || len(w.api.trashed) != 1 || w.api.trashed[0] != "sp-new" {
		t.Fatalf("err = %v, trashed = %v", err, w.api.trashed)
	}
}

func TestCreateNeedsAConnectedAccountAndQuota(t *testing.T) {
	w := newWorld(t)
	in := CreateInput{WorkspaceID: testWorkspace, Title: "Leads", Columns: []googlesheet.ColumnInput{{Header: "Nome", Type: googlesheet.TypeText}}}
	w.limiter.writes = 0
	if _, err := w.manager.Create(context.Background(), in); !errors.Is(err, googlesheet.ErrGoogleBusy) {
		t.Fatalf("over quota err = %v", err)
	}
	w.access.err = googleaccount.ErrNotConnected
	if _, err := w.manager.Create(context.Background(), in); !errors.Is(err, googleaccount.ErrNotConnected) {
		t.Fatalf("disconnected err = %v", err)
	}
}

func TestReactivateRepairsTheHeaderAndResumes(t *testing.T) {
	w := newWorld(t)
	w.enqueue(t, "k1", map[string]any{"nome": "a"})
	w.api.header = toAny([]string{"ref", "Cliente"})
	w.write(t)
	if w.stored(t).Status != googlesheet.StatusNeedsAttention {
		t.Fatal("setup: the sheet should need attention")
	}
	if _, err := w.manager.Reactivate(context.Background(), ReactivateInput{WorkspaceID: testWorkspace, ID: w.sheet.ID}); !errors.Is(err, googlesheet.ErrSheetNotActive) {
		t.Fatalf("reactivating without a repair under a changed header: %v", err)
	}
	if _, err := w.manager.Reactivate(context.Background(), ReactivateInput{WorkspaceID: testWorkspace, ID: w.sheet.ID, RepairHeader: true}); err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if w.api.applied != 1 || w.stored(t).Status != googlesheet.StatusActive || w.alerts.resolved != 1 {
		t.Fatalf("applied %d, sheet %+v, resolved %d", w.api.applied, w.stored(t), w.alerts.resolved)
	}
	w.api.header = toAny(w.sheet.HeaderRow())
	w.write(t)
	if len(w.api.written()) != 1 {
		t.Fatal("the kept row was not written after the repair")
	}
}

func TestEditColumnsRefusesAStaleVersion(t *testing.T) {
	w := newWorld(t)
	_, err := w.manager.EditColumns(context.Background(), ColumnsInput{
		WorkspaceID: testWorkspace, ID: w.sheet.ID, Version: 7,
		Edit: googlesheet.ColumnsEdit{Add: []googlesheet.ColumnInput{{Header: "Obs", Type: googlesheet.TypeText}}},
	})
	if !errors.Is(err, googlesheet.ErrStaleSheet) {
		t.Fatalf("err = %v", err)
	}
	updated, err := w.manager.EditColumns(context.Background(), ColumnsInput{
		WorkspaceID: testWorkspace, ID: w.sheet.ID, Version: 1,
		Edit: googlesheet.ColumnsEdit{Add: []googlesheet.ColumnInput{{Header: "Obs", Type: googlesheet.TypeText}}},
	})
	if err != nil || updated.Version != 2 || w.api.applied != 1 {
		t.Fatalf("EditColumns = %+v, %v, applied %d", updated, err, w.api.applied)
	}
}
