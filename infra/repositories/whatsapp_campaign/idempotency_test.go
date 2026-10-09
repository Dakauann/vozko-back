package whatsapp_campaign_repository

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

func newKeyedMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestFindByIdempotencyKeyReadsOneLiveCampaignOfTheWorkspace(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectQuery(`SELECT \* FROM "whatsapp_campaigns" WHERE \(workspace_id = \$1 AND idempotency_key = \$2\) AND "whatsapp_campaigns"."deleted_at" IS NULL ORDER BY .* LIMIT \$3`).
		WithArgs("ws-1", "base:1/1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name", "status", "source", "idempotency_key"}).
			AddRow("c-1", "ws-1", "Matrículas", "STOPPED", "lead_selection", "base:1/1"))

	found, err := NewRepository(db).(*repository).FindByIdempotencyKey("ws-1", "base:1/1")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey: %v", err)
	}
	if found.ID != "c-1" || found.Source != campaign.SourceLeadSelection || found.IdempotencyKey != "base:1/1" {
		t.Fatalf("found = %+v", found)
	}
}

func TestFindByIdempotencyKeyWithoutAMatchIsNotFound(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectQuery(`FROM "whatsapp_campaigns"`).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	if _, err := NewRepository(db).(*repository).FindByIdempotencyKey("ws-1", "base:1/1"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("err = %v, want ErrCampaignNotFound", err)
	}
}

func TestFindByIdempotencyKeyRefusesAnEmptyKey(t *testing.T) {
	db, _ := newKeyedMockDB(t)
	if _, err := NewRepository(db).(*repository).FindByIdempotencyKey("ws-1", " "); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("err = %v, want ErrCampaignNotFound", err)
	}
}

func TestCreateWritesTheSourceAndKeyAndNamesALostKeyRace(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectExec(`INSERT INTO "whatsapp_campaigns" .*"source","idempotency_key"`).
		WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "ux_wa_campaign_idem" (SQLSTATE 23505)`))

	err := NewRepository(db).Create(&wc.Campaign{ID: "c-1", WorkspaceID: "ws-1", Name: "Matrículas", Source: campaign.SourceLeadSelection, IdempotencyKey: "base:1/1"})
	if !errors.Is(err, campaign.ErrIdempotencyKeyTaken) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyTaken", err)
	}
}

func keyedDraft() *wc.Campaign {
	return &wc.Campaign{ID: "c-1", WorkspaceID: "ws-1", Name: "Matrículas", Status: wc.CampaignStatusStopped, Source: campaign.SourceLeadSelection, IdempotencyKey: "base:1/1"}
}

func keyedEntries() []wce.WhatsAppCampaignEntry {
	return []wce.WhatsAppCampaignEntry{{ID: "e-1", CampaignID: "c-1", LeadID: "lead-a", Status: wce.SendStatusPending}}
}

func TestCreateWithEntriesWritesTheKeyedCampaignAndItsEntriesInOneTransaction(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "whatsapp_campaigns" .*"idempotency_key"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "whatsapp_campaign_entries" .*ON CONFLICT \("campaign_id","lead_id"\) DO NOTHING`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := NewRepository(db).(*repository).CreateWithEntries(keyedDraft(), keyedEntries()); err != nil {
		t.Fatalf("CreateWithEntries: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithEntriesRollsTheKeyedCampaignBackWhenTheEntriesFail(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "whatsapp_campaigns"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "whatsapp_campaign_entries"`).WillReturnError(errors.New("canceling statement due to statement timeout"))
	mock.ExpectRollback()

	if err := NewRepository(db).(*repository).CreateWithEntries(keyedDraft(), keyedEntries()); err == nil {
		t.Fatal("a failed entry insert reported success")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateWithEntriesNamesALostKeyRace(t *testing.T) {
	db, mock := newKeyedMockDB(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "whatsapp_campaigns"`).
		WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "ux_wa_campaign_idem" (SQLSTATE 23505)`))
	mock.ExpectRollback()

	if err := NewRepository(db).(*repository).CreateWithEntries(keyedDraft(), keyedEntries()); !errors.Is(err, campaign.ErrIdempotencyKeyTaken) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyTaken", err)
	}
}
