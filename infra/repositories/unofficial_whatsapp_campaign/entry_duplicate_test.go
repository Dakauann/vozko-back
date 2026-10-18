package unofficial_whatsapp_campaign_repository

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func TestMovingAnEntryOntoANumberAlreadyInTheCampaignIsADuplicate(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()
	mock.ExpectExec(`UPDATE "unofficial_whatsapp_campaign_entries"`).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "idx_uwce_campaign_lead"})

	err := repo.UpdateEntryDetails("entry-1", uwc.UpdateEntryDetails{LeadID: "lead-1", Number: "5511981124410"})

	if !errors.Is(err, uwc.ErrEntryDuplicate) {
		t.Fatalf("err = %v, want the campaign's duplicate error", err)
	}
}
