package unofficial_whatsapp_repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestFindByIDs_MatchesTheLeadIDAsWellAsTheContactID(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM "unofficial_whatsapp_contacts" WHERE .*id IN .* OR lead_id IN `).
		WillReturnRows(sqlmock.NewRows([]string{"id", "lead_id", "name"}).
			AddRow("contact-1", "lead-1", "Dakauann"))

	got, err := NewContactRepository(db).FindByIDs(context.Background(), []string{"lead-1"})
	if err != nil {
		t.Fatalf("FindByIDs: %v", err)
	}
	if len(got) != 1 || got[0].ID != "contact-1" {
		t.Fatalf("a contact addressed by its lead id was not found: %+v", got)
	}
	if got[0].LeadID == nil || *got[0].LeadID != "lead-1" {
		t.Fatalf("LeadID must survive the mapping so the caller can key on it: %+v", got[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindByIDs_EmptyInputAsksNothing(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	got, err := NewContactRepository(db).FindByIDs(context.Background(), nil)
	if err != nil || got != nil {
		t.Fatalf("FindByIDs(nil) = %v, %v; want nil, nil", got, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
