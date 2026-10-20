package workflow_repository

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

func sqlPattern(query string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(query), `\?`, `\$[0-9]+`) + "$"
}

func TestADeliveryIsRecordedWithItsKeysAndNoRunWhenRejected(t *testing.T) {
	db, mock := mockDB(t)
	received := time.Date(2026, 10, 19, 20, 26, 0, 0, time.UTC)
	d := &workflow.WebhookDelivery{
		ID: "d-1", WebhookID: "wh-1", WorkflowID: "wf-1", WorkspaceID: "ws-1", ReceivedAt: received,
		Outcome: workflow.WebhookOutcomeRejected, Reason: "workflow_not_active", HTTPStatus: 409,
		EntryResolution: workflow.WebhookEntryByPhone, EntryID: "e-1", EntryType: "whatsapp", LatencyMs: 12,
		PayloadKeys: []string{"cpf", "phone"},
	}
	mock.ExpectBegin()
	mock.ExpectExec(`^INSERT INTO "workflow_webhook_deliveries" \("id","webhook_id","workflow_id","workspace_id","received_at","outcome","reason","http_status","entry_resolution","entry_id","entry_type","run_id","latency_ms","payload_keys"\) VALUES`).
		WithArgs("d-1", "wh-1", "wf-1", "ws-1", received, "rejected", "workflow_not_active", 409, "phone", "e-1", "whatsapp", nullArg{}, int64(12), jsonArg{t, `["cpf","phone"]`}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := NewWebhookDeliveryRepository(db).Record(d); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveriesAreListedNewestFirstInsideTheWorkspace(t *testing.T) {
	db, mock := mockDB(t)
	received := time.Date(2026, 10, 19, 20, 26, 0, 0, time.UTC)
	mock.ExpectQuery(sqlPattern(`SELECT count(*) FROM "workflow_webhook_deliveries" WHERE workspace_id = ? AND workflow_id = ?`)).
		WithArgs("ws-1", "wf-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(sqlPattern(`SELECT * FROM "workflow_webhook_deliveries" WHERE workspace_id = ? AND workflow_id = ? ORDER BY received_at DESC LIMIT ? OFFSET ?`)).
		WithArgs("ws-1", "wf-1", 2, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "webhook_id", "workflow_id", "workspace_id", "received_at", "outcome", "reason", "http_status", "entry_resolution", "entry_id", "entry_type", "run_id", "latency_ms", "payload_keys"}).
			AddRow("d-1", "wh-1", "wf-1", "ws-1", received, "accepted", "", 202, "entry_id", "e-1", "whatsapp", "r-1", 8, `["entry_id"]`))

	page, err := NewWebhookDeliveryRepository(db).ListByWorkflow("ws-1", "wf-1", conversation.VisibilityScope{}, shared.Pagination{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 3 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	got := page.Items[0]
	if got.RunID != "r-1" || got.Outcome != workflow.WebhookOutcomeAccepted || got.HTTPStatus != 202 || len(got.PayloadKeys) != 1 {
		t.Fatalf("delivery = %+v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOldDeliveriesAreDeletedInBoundedBatches(t *testing.T) {
	db, mock := mockDB(t)
	cutoff := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	if got := strings.Count(deleteDeliveriesBeforeSQL, "?"); got != 2 {
		t.Fatalf("placeholders = %d, want 2", got)
	}
	mock.ExpectExec(sqlPattern(deleteDeliveriesBeforeSQL)).WithArgs(cutoff, 5000).WillReturnResult(sqlmock.NewResult(0, 5000))

	deleted, err := NewWebhookDeliveryRepository(db).DeleteReceivedBefore(cutoff, 5000)
	if err != nil || deleted != 5000 {
		t.Fatalf("deleted = %d, err = %v", deleted, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
