package export_usecase

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"testing"

	"vozko/domain/analysis"
	"vozko/domain/export"
	shared_domain "vozko/domain/shared"
	"vozko/domain/stage"
)

// fakeLister replays a fixed set of rows, and records the scope it was handed
// so tests can assert the usecase pushes filtering down instead of doing it in
// memory. It also counts walks, which is how the two-pass design is pinned.
type fakeLister struct {
	entries []export.ChannelEntry
	scopes  []export.Scope
	walks   int
	err     error
}

func (f *fakeLister) ListForExport(ctx context.Context, scope export.Scope, emit func(export.ChannelEntry) error) error {
	f.walks++
	f.scopes = append(f.scopes, scope)
	if f.err != nil {
		return f.err
	}
	for _, e := range f.entries {
		// A real lister filters status in SQL; this one honours the same
		// contract so the fixtures behave like the database does.
		if len(scope.Statuses) > 0 && !containsFold(scope.Statuses, e.Status) {
			continue
		}
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

type fakeAnalysis struct {
	byEntry map[string]*analysis.Analysis
	batches [][]string
}

func (f *fakeAnalysis) FindLatestByEntries(entryIDs []string, _ shared_domain.EntryType) (map[string]*analysis.Analysis, error) {
	f.batches = append(f.batches, append([]string(nil), entryIDs...))
	out := make(map[string]*analysis.Analysis, len(entryIDs))
	for _, id := range entryIDs {
		if a, ok := f.byEntry[id]; ok {
			out[id] = a
		}
	}
	return out, nil
}

type fakeStages struct {
	byEntry map[string]*stage.EntryStage
}

func (f *fakeStages) GetBatchEntryStages(entryIDs []string, _, _ string) (map[string]*stage.EntryStage, error) {
	out := make(map[string]*stage.EntryStage, len(entryIDs))
	for _, id := range entryIDs {
		if s, ok := f.byEntry[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func newUseCase(t *testing.T, lister *fakeLister, opts ...func(*fakeAnalysis, *fakeStages)) (export.ExportEntriesUseCase, *fakeAnalysis) {
	t.Helper()
	an := &fakeAnalysis{byEntry: map[string]*analysis.Analysis{}}
	st := &fakeStages{byEntry: map[string]*stage.EntryStage{}}
	for _, opt := range opts {
		opt(an, st)
	}

	uc := NewExportEntriesUseCase(an, st)
	uc.(*exportEntriesUseCase).SetChannelEntryLister(export.EntryTypeWhatsApp, lister)
	return uc, an
}

func whatsappEntries(n int) []export.ChannelEntry {
	out := make([]export.ChannelEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, export.ChannelEntry{
			EntryID:       fmt.Sprintf("e-%d", i),
			Number:        fmt.Sprintf("5511%07d", i),
			Name:          fmt.Sprintf("Lead %d", i),
			ContainerName: "Campanha A",
			Status:        "DELIVERED",
			CreatedAt:     "2026-08-01T10:00:00Z",
			UpdatedAt:     "2026-08-01T11:00:00Z",
		})
	}
	return out
}

func exportToCSV(t *testing.T, uc export.ExportEntriesUseCase, filter export.ExportFilter) (int, [][]string) {
	t.Helper()
	var buf strings.Builder
	count, err := uc.Export(context.Background(), filter, &buf)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if buf.Len() == 0 {
		return count, nil
	}
	records, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return count, records
}

func whatsappFilter(scope export.Scope) export.ExportFilter {
	scope.WorkspaceID = "ws-1"
	return export.ExportFilter{Scope: scope, EntryType: export.EntryTypeWhatsApp}
}

// The card: pull the leads that were sent, delivered and read, in one file,
// across every campaign. The statuses have to reach the lister so the database
// does the narrowing — filtering 22k rows down to 6.6k in Go would drag the
// whole workspace through the process on every export.
func TestExportPushesStatusFilterIntoTheScope(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{
		{EntryID: "e-1", Number: "5511900000001", Status: "SENT"},
		{EntryID: "e-2", Number: "5511900000002", Status: "DELIVERED"},
		{EntryID: "e-3", Number: "5511900000003", Status: "READ"},
		{EntryID: "e-4", Number: "5511900000004", Status: "PENDING"},
		{EntryID: "e-5", Number: "5511900000005", Status: "FAILED"},
	}}
	uc, _ := newUseCase(t, lister)

	count, records := exportToCSV(t, uc, whatsappFilter(export.Scope{
		Statuses: []string{"SENT", "DELIVERED", "READ"},
	}))

	if count != 3 {
		t.Errorf("wrote %d rows, want 3", count)
	}
	if len(records) != 4 { // header + 3
		t.Fatalf("csv has %d lines, want 4", len(records))
	}
	for _, scope := range lister.scopes {
		if len(scope.Statuses) != 3 {
			t.Errorf("lister received %d statuses, want 3 — filtering did not reach the query", len(scope.Statuses))
		}
	}
}

func TestExportWithoutStatusesReturnsEverything(t *testing.T) {
	lister := &fakeLister{entries: whatsappEntries(7)}
	uc, _ := newUseCase(t, lister)

	count, _ := exportToCSV(t, uc, whatsappFilter(export.Scope{}))
	if count != 7 {
		t.Errorf("wrote %d rows, want 7", count)
	}
}

// A file spanning campaigns has to say which campaign each row came from, or a
// row cannot be traced back to what produced it. A single-campaign file already
// knows, and the extra column would only be noise.
func TestCampaignColumnAppearsOnlyWhenTheScopeSpansCampaigns(t *testing.T) {
	entries := []export.ChannelEntry{{
		EntryID: "e-1", Number: "5511900000001", Name: "Ana",
		ContainerName: "Cobrança Julho", Status: "READ",
	}}

	t.Run("workspace wide", func(t *testing.T) {
		uc, _ := newUseCase(t, &fakeLister{entries: entries})
		_, records := exportToCSV(t, uc, whatsappFilter(export.Scope{}))

		if records[0][0] != "campaign" {
			t.Fatalf("first column is %q, want campaign", records[0][0])
		}
		if records[1][0] != "Cobrança Julho" {
			t.Errorf("campaign cell is %q", records[1][0])
		}
	})

	t.Run("single campaign", func(t *testing.T) {
		uc, _ := newUseCase(t, &fakeLister{entries: entries})
		_, records := exportToCSV(t, uc, whatsappFilter(export.Scope{ContainerID: "camp-1"}))

		if records[0][0] != "number" {
			t.Errorf("first column is %q, want number", records[0][0])
		}
		for _, col := range records[0] {
			if col == "campaign" {
				t.Error("single-campaign export carries a campaign column")
			}
		}
	})
}

// Nothing at all is written when nothing matches — not even a header. A file
// with only a header is not "no results", it is an empty spreadsheet the
// operator has to open to find that out, and the caller can no longer answer
// with a status code once a byte is on the wire.
func TestExportWritesNothingWhenNothingMatches(t *testing.T) {
	lister := &fakeLister{entries: whatsappEntries(3)}
	uc, _ := newUseCase(t, lister)

	var buf strings.Builder
	count, err := uc.Export(context.Background(), whatsappFilter(export.Scope{
		Statuses: []string{"FAILED"},
	}), &buf)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes for an empty result: %q", buf.Len(), buf.String())
	}
}

// Over the cap the export refuses rather than truncating: a file that silently
// stops at 50k looks complete and gets acted on as if it were.
func TestExportRefusesScopesOverTheRowCap(t *testing.T) {
	lister := &fakeLister{entries: whatsappEntries(maxExportRows + 1)}
	uc, _ := newUseCase(t, lister)

	var buf strings.Builder
	_, err := uc.Export(context.Background(), whatsappFilter(export.Scope{}), &buf)
	if !errors.Is(err, export.ErrTooManyRows) {
		t.Fatalf("err = %v, want ErrTooManyRows", err)
	}
	if buf.Len() != 0 {
		t.Error("wrote bytes before refusing; the cap must be decided before the response starts")
	}
	if lister.walks != 1 {
		t.Errorf("walked %d times, want 1 — the cap should stop the measuring pass", lister.walks)
	}
}

// Memory has to stay flat as the scope grows, which means enrichment reads a
// bounded window of ids at a time rather than one IN clause holding every row.
func TestEnrichmentReadsInBoundedBatches(t *testing.T) {
	const rows = enrichBatchSize*2 + 25
	lister := &fakeLister{entries: whatsappEntries(rows)}
	uc, an := newUseCase(t, lister)

	count, _ := exportToCSV(t, uc, whatsappFilter(export.Scope{}))
	if count != rows {
		t.Fatalf("wrote %d rows, want %d", count, rows)
	}

	if len(an.batches) != 3 {
		t.Errorf("made %d analysis lookups, want 3", len(an.batches))
	}
	total := 0
	for _, batch := range an.batches {
		if len(batch) > enrichBatchSize {
			t.Errorf("a batch held %d ids, over the %d limit", len(batch), enrichBatchSize)
		}
		total += len(batch)
	}
	if total != rows {
		t.Errorf("batches covered %d ids, want %d", total, rows)
	}
}

func TestExportWalksTwiceAndOnlyTwice(t *testing.T) {
	lister := &fakeLister{entries: whatsappEntries(3)}
	uc, _ := newUseCase(t, lister)

	exportToCSV(t, uc, whatsappFilter(export.Scope{}))
	if lister.walks != 2 {
		t.Errorf("walked %d times, want 2 (measure then stream)", lister.walks)
	}
}

// Template variables are per-campaign, so the header is as wide as the widest
// row and narrower rows are padded rather than short — a ragged CSV does not
// parse.
func TestVariableColumnsArePaddedToTheWidestRow(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{
		{EntryID: "e-1", Number: "5511900000001", Status: "READ", Variables: []string{"a"}},
		{EntryID: "e-2", Number: "5511900000002", Status: "READ", Variables: []string{"a", "b", "c"}},
	}}
	uc, _ := newUseCase(t, lister)

	_, records := exportToCSV(t, uc, whatsappFilter(export.Scope{ContainerID: "camp-1"}))

	width := len(records[0])
	for i, row := range records {
		if len(row) != width {
			t.Errorf("row %d has %d cells, header has %d", i, len(row), width)
		}
	}
	if !strings.Contains(strings.Join(records[0], ","), "variable_3") {
		t.Errorf("header does not reach variable_3: %v", records[0])
	}
}

// Every free-text cell in this file is written by someone outside the system —
// contact names, campaign names, uploaded metadata, AI summaries. Excel and
// Sheets execute a cell that starts with =, +, - or @, so a lead named
// "=cmd|'/c calc'!A1" would run on the machine of whoever opens the export.
func TestFormulaInjectionIsNeutralisedInTextCells(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{{
		EntryID:       "e-1",
		Number:        "+5511900000001",
		Name:          `=cmd|'/c calc'!A1`,
		ContainerName: "@campanha",
		Status:        "READ",
	}}}
	uc, _ := newUseCase(t, lister)

	_, records := exportToCSV(t, uc, whatsappFilter(export.Scope{}))
	row := records[1]

	if got := row[0]; got != "'@campanha" {
		t.Errorf("campaign cell = %q, want it prefixed out of formula position", got)
	}
	if got := row[2]; !strings.HasPrefix(got, "'") {
		t.Errorf("name cell = %q, want it prefixed out of formula position", got)
	}

	// The number column is the one operators paste into dialers. It is already
	// reduced to digits and a leading +, which cannot carry a payload, so it
	// must come through untouched.
	if got := row[1]; got != "+5511900000001" {
		t.Errorf("number cell = %q, want the raw number", got)
	}
}

func TestNewlinesInTextDoNotBreakTheRecord(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{{
		EntryID: "e-1", Number: "5511900000001", Status: "READ",
		Name: "Ana\nMaria\r\nSilva",
	}}}
	uc, _ := newUseCase(t, lister)

	count, records := exportToCSV(t, uc, whatsappFilter(export.Scope{}))
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
	if len(records) != 2 {
		t.Fatalf("csv has %d lines, want 2 — a name split the record", len(records))
	}
	if strings.ContainsAny(records[1][2], "\r\n") {
		t.Errorf("name cell still holds a line break: %q", records[1][2])
	}
}

// The status re-check is deliberate belt-and-braces: the port cannot force a
// lister to honour Scope.Statuses, and an export that quietly includes statuses
// the operator excluded is worse than one that costs a comparison per row.
func TestStatusIsReCheckedEvenIfAListerIgnoresTheScope(t *testing.T) {
	leaky := &leakyLister{entries: []export.ChannelEntry{
		{EntryID: "e-1", Number: "5511900000001", Status: "READ"},
		{EntryID: "e-2", Number: "5511900000002", Status: "FAILED"},
	}}
	an := &fakeAnalysis{byEntry: map[string]*analysis.Analysis{}}
	st := &fakeStages{byEntry: map[string]*stage.EntryStage{}}
	uc := NewExportEntriesUseCase(an, st)
	uc.(*exportEntriesUseCase).SetChannelEntryLister(export.EntryTypeWhatsApp, leaky)

	count, _ := exportToCSV(t, uc, whatsappFilter(export.Scope{Statuses: []string{"READ"}}))
	if count != 1 {
		t.Errorf("wrote %d rows, want 1 — a leaky lister got through", count)
	}
}

type leakyLister struct{ entries []export.ChannelEntry }

func (l *leakyLister) ListForExport(_ context.Context, _ export.Scope, emit func(export.ChannelEntry) error) error {
	for _, e := range l.entries {
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

func TestExportRequiresAWorkspace(t *testing.T) {
	uc, _ := newUseCase(t, &fakeLister{})
	var buf strings.Builder

	_, err := uc.Export(context.Background(), export.ExportFilter{
		EntryType: export.EntryTypeWhatsApp,
	}, &buf)
	if err == nil {
		t.Fatal("exported without a workspace id")
	}
}

func TestUnregisteredChannelIsRejected(t *testing.T) {
	uc, _ := newUseCase(t, &fakeLister{})
	var buf strings.Builder

	_, err := uc.Export(context.Background(), export.ExportFilter{
		Scope:     export.Scope{WorkspaceID: "ws-1"},
		EntryType: export.EntryTypeInstagram,
	}, &buf)
	if err == nil {
		t.Fatal("exported a channel with no registered lister")
	}
}

func TestListerErrorsSurfaceBeforeAnyBytes(t *testing.T) {
	boom := errors.New("boom")
	uc, _ := newUseCase(t, &fakeLister{err: boom})

	var buf strings.Builder
	_, err := uc.Export(context.Background(), whatsappFilter(export.Scope{}), &buf)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if buf.Len() != 0 {
		t.Error("wrote bytes despite failing to read rows")
	}
}
