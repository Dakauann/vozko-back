package whatsapp_campaign_entry

import (
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func newReportReader(t *testing.T) (wce.DispatchReportReader, sqlmock.Sqlmock, func()) {
	t.Helper()
	repo, mock, sqlDB := newEntryDB(t)
	return NewDispatchReportReader(repo.db), mock, func() { sqlDB.Close() }
}

func TestFunnelCountsEveryStageFromMilestonesOrTheStatusThatProvesThem(t *testing.T) {
	reader, mock, done := newReportReader(t)
	defer done()
	since := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) AS base,.*` +
		`e\.sent_at IS NOT NULL OR e\.status IN \(.+,.+,.+\)\) AS sent,.*` +
		`e\.delivered_at IS NOT NULL OR e\.status IN \(.+,.+\)\) AS delivered,.*` +
		`e\.read_at IS NOT NULL OR e\.status IN \(.+\)\) AS read,.*` +
		`EXISTS \(\s*SELECT 1 FROM conversation_messages m\s+WHERE m\.entry_id = e\.id AND m\.entry_type = .+ AND m\.deleted_at IS NULL AND m\.sender_kind = 'contact'\s*\)\) AS replied,.*` +
		`MIN\(e\.sent_at\) AS tracked_since\s+FROM whatsapp_campaign_entries e\s+WHERE e\.campaign_id = .+ AND e\.deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{
			"base", "sent", "delivered", "read", "replied", "failed", "awaiting_delivery", "pending", "not_eligible", "tracked_since",
		}).AddRow(10, 9, 8, 6, 3, 1, 1, 0, 0, since))

	got, err := reader.Funnel(campaignScope(t))
	if err != nil {
		t.Fatalf("Funnel() error = %v", err)
	}
	want := wce.Funnel{Base: 10, Sent: 9, Delivered: 8, Read: 6, Replied: 3, Failed: 1, AwaitingDelivery: 1, TrackedSince: &since}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Funnel() = %+v, want %+v", got, want)
	}
}

func TestDailyBucketsFirstMomentsIntoLocalDaysInsideTheWindow(t *testing.T) {
	reader, mock, done := newReportReader(t)
	defer done()
	window, err := wce.NewDayWindow("2026-09-01", "2026-09-02", time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(`(?s)WITH scoped AS \(.*FROM whatsapp_campaign_entries e\s+WHERE e\.campaign_id = .+ AND e\.deleted_at IS NULL.*` +
		`CROSS JOIN LATERAL \(\s*SELECT MIN\(m\.created_at\) AS first_at.*` +
		`to_char\(happened_at AT TIME ZONE .+, 'YYYY-MM-DD'\) AS day.*` +
		`WHERE happened_at >= .+ AND happened_at < .+\s+GROUP BY 1\s+ORDER BY 1`).
		WillReturnRows(sqlmock.NewRows([]string{"day", "sent", "delivered", "read", "replied"}).
			AddRow("2026-09-02", 9, 8, 6, 3))

	got, err := reader.Daily(campaignScope(t), window)
	if err != nil {
		t.Fatalf("Daily() error = %v", err)
	}
	if want := []wce.DayCount{{Day: "2026-09-02", Sent: 9, Delivered: 8, Read: 6, Replied: 3}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Daily() = %+v, want %+v", got, want)
	}
}

func TestFailureReasonsGroupFailedEntriesByErrorCode(t *testing.T) {
	reader, mock, done := newReportReader(t)
	defer done()

	mock.ExpectQuery(`(?s)SELECT e\.error_code AS code, COUNT\(\*\) AS count\s+FROM whatsapp_campaign_entries e\s+`+
		`WHERE e\.campaign_id = .+ AND e\.deleted_at IS NULL AND e\.status = .+\s+GROUP BY e\.error_code\s+ORDER BY count DESC, code`).
		WithArgs("c1", "FAILED").
		WillReturnRows(sqlmock.NewRows([]string{"code", "count"}).AddRow(131026, 4).AddRow(0, 1))

	got, err := reader.FailureReasons(campaignScope(t))
	if err != nil {
		t.Fatalf("FailureReasons() error = %v", err)
	}
	if want := []wce.FailureReasonCount{{Code: 131026, Count: 4}, {Code: 0, Count: 1}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("FailureReasons() = %+v, want %+v", got, want)
	}
}

func TestTagsCountDistinctCampaignConversationsPerLiveLabel(t *testing.T) {
	reader, mock, done := newReportReader(t)
	defer done()

	mock.ExpectQuery(`(?s)SELECT l\.id AS label_id, l\.name, l\.color, COUNT\(DISTINCT el\.entry_id\) AS count\s+`+
		`FROM entry_labels el\s+`+
		`JOIN labels l ON l\.id = el\.label_id::text AND l\.deleted_at IS NULL\s+`+
		`JOIN whatsapp_campaign_entries e ON e\.id = el\.entry_id AND e\.deleted_at IS NULL\s+`+
		`WHERE e\.campaign_id = .+ AND el\.entry_type = .+ AND el\.deleted_at IS NULL\s+`+
		`GROUP BY l\.id, l\.name, l\.color\s+ORDER BY count DESC, l\.name\s+LIMIT .+`).
		WithArgs("c1", "whatsapp", 10).
		WillReturnRows(sqlmock.NewRows([]string{"label_id", "name", "color", "count"}).AddRow("l1", "interessado", "#00D09A", 7))

	got, err := reader.Tags(campaignScope(t), 10)
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}
	if want := []wce.TagCount{{LabelID: "l1", Name: "interessado", Color: "#00D09A", Count: 7}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Tags() = %+v, want %+v", got, want)
	}
}

func campaignScope(t *testing.T) wce.ReportScope {
	t.Helper()
	scope, err := wce.CampaignScope("c1")
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestWorkspaceScopeCoversTheCallersCampaignsAddedInThePeriod(t *testing.T) {
	reader, mock, done := newReportReader(t)
	defer done()
	window, err := wce.NewDayWindow("2026-09-01", "2026-09-30", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := wce.WorkspaceScope("ws1", []string{"d1"}, "organic", window)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(`(?s)FROM whatsapp_campaign_entries e\s+WHERE e\.campaign_id IN \(\s*` +
		`SELECT c\.id FROM whatsapp_campaigns c\s+` +
		`WHERE c\.workspace_id = .+ AND c\.deleted_at IS NULL AND COALESCE\(c\.type, ''\) <> .+ AND c\.department_id IN .+\s+\) ` +
		`AND e\.created_at >= .+ AND e\.created_at < .+ AND e\.deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"base"}).AddRow(4))

	got, err := reader.Funnel(scope)
	if err != nil || got.Base != 4 {
		t.Fatalf("Funnel() = %+v, %v", got, err)
	}
}

func TestCampaignsBreakTheScopeDownPerCampaignLargestFirst(t *testing.T) {
	reader, mock, done := newReportReader(t)
	defer done()

	mock.ExpectQuery(`(?s)SELECT e\.campaign_id, c\.name AS campaign_name, COUNT\(\*\) AS base,.*` +
		`JOIN whatsapp_campaigns c ON c\.id = e\.campaign_id.*` +
		`GROUP BY e\.campaign_id, c\.name\s+ORDER BY base DESC, c\.name\s+LIMIT .+`).
		WillReturnRows(sqlmock.NewRows([]string{"campaign_id", "campaign_name", "base", "sent", "delivered", "read", "replied", "failed"}).
			AddRow("c1", "Lançamento", 10, 9, 8, 6, 3, 1))

	got, err := reader.Campaigns(campaignScope(t), 50)
	if err != nil {
		t.Fatalf("Campaigns() error = %v", err)
	}
	want := []wce.CampaignFunnel{{CampaignID: "c1", CampaignName: "Lançamento", Base: 10, Sent: 9, Delivered: 8, Read: 6, Replied: 3, Failed: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Campaigns() = %+v, want %+v", got, want)
	}
}
