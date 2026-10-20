package unofficial_whatsapp_campaign_repository

import (
	"errors"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/campaign"
)

func TestEachAutomationMetadataStreamsTheSavedEntries(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT lead_id, number, metadata FROM "unofficial_whatsapp_campaign_entries" WHERE campaign_id = \$1 AND "unofficial_whatsapp_campaign_entries"\."deleted_at" IS NULL`).
		WithArgs("camp-1").
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "number", "metadata"}).
			AddRow("lead-1", "5511999990001", `{"escola":"Prisma"}`).
			AddRow(nil, "5511999990002", `{}`))

	var got []campaign.EntryMetadata
	err := repo.EachAutomationMetadata("camp-1", func(entry campaign.EntryMetadata) error {
		got = append(got, entry)
		return nil
	})
	if err != nil {
		t.Fatalf("EachAutomationMetadata = %v", err)
	}
	want := []campaign.EntryMetadata{
		campaign.EntryMetadataOf("lead-1", "5511999990001", map[string]any{"escola": "Prisma"}),
		campaign.EntryMetadataOf("", "5511999990002", map[string]any{}),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visited %+v, want %+v", got, want)
	}
}

func TestEachAutomationMetadataStopsAtTheFirstRefusal(t *testing.T) {
	repo, mock, sqlDB := newEntryDB(t)
	defer sqlDB.Close()
	refusal := errors.New("missing variable")

	mock.ExpectQuery(`SELECT lead_id, number, metadata FROM "unofficial_whatsapp_campaign_entries"`).
		WillReturnRows(sqlmock.NewRows([]string{"lead_id", "number", "metadata"}).
			AddRow("lead-1", "5511999990001", `{}`).
			AddRow("lead-2", "5511999990002", `{}`))

	visits := 0
	if err := repo.EachAutomationMetadata("camp-1", func(campaign.EntryMetadata) error { visits++; return refusal }); !errors.Is(err, refusal) || visits != 1 {
		t.Fatalf("EachAutomationMetadata = %v after %d visits", err, visits)
	}
}
