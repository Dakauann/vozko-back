package export_usecase

import (
	"testing"

	"vozko/domain/export"
)

// column returns the cell under a named header, so these tests keep working
// when an optional column group shifts every index along.
func column(t *testing.T, records [][]string, row int, name string) string {
	t.Helper()
	for i, h := range records[0] {
		if h == name {
			return records[row][i]
		}
	}
	t.Fatalf("no %q column in header %v", name, records[0])
	return ""
}

func hasColumn(records [][]string, name string) bool {
	for _, h := range records[0] {
		if h == name {
			return true
		}
	}
	return false
}

// The card: the campaign detail screen shows an operator WHY a send failed, and
// the export they take to their manager did not. A file listing failures with
// no reason is a list of questions.
func TestExportCarriesTheFailureReason(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{
		{
			EntryID: "e-1", Number: "5511900000001", Name: "Ana",
			Status: "FAILED", FailureCode: 131026, FailureReason: "Receiver is incapable of receiving this message",
		},
		{EntryID: "e-2", Number: "5511900000002", Name: "Bruno", Status: "DELIVERED"},
	}}
	uc, _ := newUseCase(t, lister)

	_, records := exportToCSV(t, uc, whatsappFilter(export.Scope{}))

	if got := column(t, records, 1, "failure_code"); got != "131026" {
		t.Errorf("failure_code is %q, want 131026", got)
	}
	if got := column(t, records, 1, "failure_reason"); got != "Receiver is incapable of receiving this message" {
		t.Errorf("failure_reason is %q", got)
	}

	// A delivered row leaves both blank. Writing the column's zero default
	// would put a 0 beside every success, which an operator has to learn to
	// read as "no error", and a sorted spreadsheet would file it with the
	// real codes.
	if got := column(t, records, 2, "failure_code"); got != "" {
		t.Errorf("delivered row has failure_code %q, want blank", got)
	}
	if got := column(t, records, 2, "failure_reason"); got != "" {
		t.Errorf("delivered row has failure_reason %q, want blank", got)
	}
}

// A channel whose Status is a CONVERSATION status has no send to fail, so the
// columns are absent rather than present and permanently empty: two blank cells
// on every line read as "nothing went wrong", not as "this file cannot say".
func TestConversationChannelsHaveNoFailureColumns(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{
		{EntryID: "c-1", Number: "@ana", Name: "Ana", Status: "open"},
	}}
	uc, _ := newUseCase(t, lister)
	uc.(*exportEntriesUseCase).SetChannelEntryLister(export.EntryTypeInstagram, lister)

	_, records := exportToCSV(t, uc, export.ExportFilter{
		Scope:     export.Scope{WorkspaceID: "ws-1", ContainerID: "acc-1"},
		EntryType: export.EntryTypeInstagram,
	})

	if hasColumn(records, "failure_code") || hasColumn(records, "failure_reason") {
		t.Errorf("instagram export carries failure columns: %v", records[0])
	}
}

// A campaign target that failed before a conversation existed has no entry id
// at all. Both enrichment lookups key on a uuid column, so an empty id is not a
// miss: it is a query that errors and takes the whole export down with it.
func TestRowsWithoutAnEntryIDDoNotReachTheEnrichmentLookups(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{
		{EntryID: "", Number: "5511900000001", Status: "FAILED", FailureReason: "not on whatsapp"},
		{EntryID: "conv-2", Number: "5511900000002", Status: "SENT"},
	}}
	uc, analyses := newUseCase(t, lister)

	count, records := exportToCSV(t, uc, whatsappFilter(export.Scope{}))

	if count != 2 {
		t.Fatalf("wrote %d rows, want 2. The failed target belongs in the file", count)
	}
	if got := column(t, records, 1, "failure_reason"); got != "not on whatsapp" {
		t.Errorf("failure_reason is %q", got)
	}
	for _, batch := range analyses.batches {
		for _, id := range batch {
			if id == "" {
				t.Fatalf("an empty entry id reached the analysis lookup: %v", batch)
			}
		}
	}
}

// And when NO row in a batch has an id, the lookups are skipped entirely rather
// than asked for an empty set.
func TestBatchWithNoEntryIDsSkipsTheLookups(t *testing.T) {
	lister := &fakeLister{entries: []export.ChannelEntry{
		{EntryID: "", Number: "5511900000001", Status: "FAILED", FailureReason: "not on whatsapp"},
	}}
	uc, analyses := newUseCase(t, lister)

	count, _ := exportToCSV(t, uc, whatsappFilter(export.Scope{}))

	if count != 1 {
		t.Fatalf("wrote %d rows, want 1", count)
	}
	if len(analyses.batches) != 0 {
		t.Errorf("analysis lookup ran with nothing to look up: %v", analyses.batches)
	}
}
