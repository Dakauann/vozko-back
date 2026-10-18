package whatsapp_campaign_repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

var due = time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)

func TestClaimingAScheduleClearsItOnlyWhileItIsStillDue(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectExec(`UPDATE "whatsapp_campaigns" SET "scheduled_start"=\$1,"updated_at"=\$2 WHERE \(id = \$3 AND scheduled_start > \$4 AND scheduled_start <= \$5\) AND "whatsapp_campaigns"."deleted_at" IS NULL`).
		WithArgs(time.Time{}, sqlmock.AnyArg(), "c-1", time.Time{}, due).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "whatsapp_campaigns" SET "scheduled_start"=\$1,"updated_at"=\$2 WHERE`).
		WithArgs(time.Time{}, sqlmock.AnyArg(), "c-1", time.Time{}, due).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := NewRepository(db).(*repository)
	first, err := repo.ClaimScheduledStart("c-1", due)
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v", first, err)
	}
	second, err := repo.ClaimScheduledStart("c-1", due)
	if err != nil || second {
		t.Fatalf("second claim = %v, %v, want lost", second, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyStoppedCampaignsAreDueForAScheduledStart(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "whatsapp_campaigns" WHERE scheduled_start <= \$1 AND scheduled_start > \$2 AND status = \$3 AND "whatsapp_campaigns"."deleted_at" IS NULL ORDER BY scheduled_start ASC LIMIT \$4`).
		WithArgs(due, time.Time{}, "STOPPED", 200).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}))

	if _, err := NewRepository(db).ListScheduledToStart(due, 200); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
