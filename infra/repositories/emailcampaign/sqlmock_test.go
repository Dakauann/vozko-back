package emailcampaign

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	domain "vozko/domain/emailcampaign"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestSwapStatusIsACompareAndSwap(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "email_campaigns" SET "hold_reason"=$1,"status"=$2,"updated_at"=$3 WHERE id = $4 AND status = $5`)).
		WithArgs("daily_cap", "held", sqlmock.AnyArg(), "c1", "running").
		WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := NewCampaignRepository(db).SwapStatus(context.Background(), "c1", domain.StatusRunning, domain.StatusHeld, domain.HoldDailyCap)
	if err != nil || !ok {
		t.Fatalf("SwapStatus() = %v, %v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetWinnerOnlyOnce(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`jsonb_set(ab_test, '{winner}', to_jsonb($1::text))`)+`.*`+regexp.QuoteMeta(`COALESCE(ab_test->>'winner', '') = ''`)).
		WithArgs("b", sqlmock.AnyArg(), "c1").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err := NewCampaignRepository(db).SetWinner(context.Background(), "c1", domain.VariantB)
	if err != nil || ok {
		t.Fatalf("SetWinner() = %v, %v; a decided test is never decided again", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimScheduledStartClearsTheSchedule(t *testing.T) {
	db, mock := newMockDB(t)
	due := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "email_campaigns" SET "scheduled_at"=$1,"updated_at"=$2 WHERE id = $3 AND status = $4 AND scheduled_at IS NOT NULL AND scheduled_at <= $5`)).
		WithArgs(nil, sqlmock.AnyArg(), "c1", "scheduled", due).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := NewCampaignRepository(db).ClaimScheduledStart("c1", due)
	if err != nil || !ok {
		t.Fatalf("Claim() = %v, %v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReplaceInsertsInBatchesInsideOneTransaction(t *testing.T) {
	db, mock := newMockDB(t)
	recipients := make([]domain.Recipient, recipientInsertBatch+1)
	for i := range recipients {
		recipients[i] = domain.Recipient{ID: uuid.NewString(), Address: "a@x.com", Status: domain.RecipientPending, Variant: domain.VariantA}
	}
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "email_campaign_recipients" WHERE campaign_id = $1`)).WithArgs("c1").WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "email_campaign_recipients"`)).WillReturnResult(sqlmock.NewResult(0, recipientInsertBatch))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "email_campaign_recipients"`)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := NewRecipientRepository(db).Replace(context.Background(), "ws", "c1", recipients); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkSentOnlyMovesPendingRecipients(t *testing.T) {
	db, mock := newMockDB(t)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "email_campaign_recipients" SET "message_id"=$1,"sent_at"=$2,"status"=$3,"updated_at"=$4 WHERE id = $5 AND status = $6`)).
		WithArgs("m1", sqlmock.AnyArg(), "sent", sqlmock.AnyArg(), "r1", "pending").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err := NewRecipientRepository(db).MarkSent(context.Background(), "r1", "m1", time.Now())
	if err != nil || ok {
		t.Fatalf("MarkSent() = %v, %v", ok, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMetricQueriesScopeToTheCampaignRun(t *testing.T) {
	for name, sql := range map[string]string{"tally": campaignMessagesSQL, "timeline": timelineSQL, "links": linksSQL, "domains": domainsSQL} {
		if !regexp.MustCompile(regexp.QuoteMeta(`m.workspace_id = ? AND m.run_kind = 'campaign' AND m.run_id = ?`)).MatchString(sql) {
			t.Errorf("%s query is not scoped to the campaign run", name)
		}
	}
	if !regexp.MustCompile(`NOT e\.is_bot`).MatchString(linksSQL) {
		t.Error("bot clicks must not count")
	}
}
