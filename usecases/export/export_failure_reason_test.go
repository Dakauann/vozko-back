package export_usecase

import (
	"testing"

	"vozko/domain/export"
)

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

	if got := column(t, records, 2, "failure_code"); got != "" {
		t.Errorf("delivered row has failure_code %q, want blank", got)
	}
	if got := column(t, records, 2, "failure_reason"); got != "" {
		t.Errorf("delivered row has failure_reason %q, want blank", got)
	}
}

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
