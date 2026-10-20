package whatsapp_campaign_entry

import (
	"errors"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	wce "vozko/domain/whatsapp_campaign_entry"
)

func TestEachStoredStreamsOnlyWhatTheEditChecksNeed(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT lead_id, status, variables, metadata FROM "whatsapp_campaign_entries" WHERE campaign_id = \$1 AND "whatsapp_campaign_entries"\."deleted_at" IS NULL`).
		WithArgs("camp-1").
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "status", "variables", "metadata"}).
			AddRow("lead-1", "PENDING", "{Bia}", `{"escola":"Prisma"}`).
			AddRow("lead-2", "SENT", "{}", `{}`))

	var got []wce.StoredEntry
	err := repo.EachStored("camp-1", func(entry wce.StoredEntry) error {
		got = append(got, entry)
		return nil
	})
	if err != nil {
		t.Fatalf("EachStored = %v", err)
	}
	want := []wce.StoredEntry{
		{LeadID: "lead-1", Status: wce.SendStatusPending, Variables: []string{"Bia"}, Metadata: map[string]interface{}{"escola": "Prisma"}},
		{LeadID: "lead-2", Status: wce.SendStatusSent, Variables: []string{}, Metadata: map[string]interface{}{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EachStored visited %+v, want %+v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEachStoredStopsWhenTheVisitorRefuses(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()
	refusal := errors.New("missing variable")

	mock.ExpectQuery(`SELECT lead_id, status, variables, metadata FROM "whatsapp_campaign_entries"`).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "status", "variables", "metadata"}).
			AddRow("lead-1", "PENDING", "{}", `{}`).
			AddRow("lead-2", "PENDING", "{}", `{}`))

	visits := 0
	err := repo.EachStored("camp-1", func(wce.StoredEntry) error {
		visits++
		return refusal
	})
	if !errors.Is(err, refusal) || visits != 1 {
		t.Fatalf("EachStored = %v after %d visits, want the refusal after one", err, visits)
	}
}

func TestEachStoredNeedsACampaign(t *testing.T) {
	repo, _, sqlDB := newEntryDB(t)
	defer sqlDB.Close()
	if err := repo.EachStored(" ", func(wce.StoredEntry) error { return nil }); !errors.Is(err, wce.ErrEntryCampaignRequired) {
		t.Fatalf("EachStored = %v, want ErrEntryCampaignRequired", err)
	}
}
