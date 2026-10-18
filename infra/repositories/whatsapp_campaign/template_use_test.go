package whatsapp_campaign_repository

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestATemplateIsBusyWhileACampaignRunsOrWaitsForItsScheduledStart(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM "whatsapp_campaigns" WHERE \(template_id = \$1 AND \(status = \$2 OR \(status = \$3 AND scheduled_start > \$4\)\)\) AND "whatsapp_campaigns"."deleted_at" IS NULL`).
		WithArgs("tpl-1", "RUNNING", "STOPPED", due).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	busy, err := NewTemplateUse(db).BusyWithTemplate("tpl-1", due)

	if err != nil || !busy {
		t.Fatalf("busy = %v, %v", busy, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
