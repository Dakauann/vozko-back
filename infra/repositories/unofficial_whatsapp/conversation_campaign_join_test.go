package unofficial_whatsapp_repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	conversation_repository "vozko/infra/repositories/conversation"
)

func TestConversationCampaignJoinReadsTheStoredCampaign(t *testing.T) {
	// The campaign is known the moment its conversation is created. Searching
	// the entries instead left a window before the send was recorded in which a
	// campaign's conversation followed the instance's AI.
	join := conversation_repository.UnofficialCampaignJoin("uwc", "camp")
	for _, want := range []string{
		"LEFT JOIN unofficial_whatsapp_campaigns camp",
		"camp.id = NULLIF(uwc.campaign_id, '')::uuid",
		"camp.deleted_at IS NULL",
	} {
		if !strings.Contains(join, want) {
			t.Errorf("join is missing %q:\n%s", want, join)
		}
	}
	if strings.Contains(join, "campaign_entries") {
		t.Error("the join must not search campaign entries")
	}
}

func TestCampaignIDForEntryReadsTheConversationsCampaign(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM unofficial_whatsapp_conversations uwc LEFT JOIN unofficial_whatsapp_campaigns camp ON camp\.id = NULLIF\(uwc\.campaign_id, ''\)::uuid`).
		WithArgs("conv-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("camp-1"))

	got, err := NewConversationRepository(db).CampaignIDForEntry(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "camp-1" {
		t.Fatalf("campaign = %q, want camp-1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDepartmentIDForEntryFollowsTheConversationsCampaign(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`LEFT JOIN unofficial_whatsapp_campaigns camp ON camp\.id = NULLIF\(uwc\.campaign_id, ''\)::uuid.*COALESCE\(camp\.department_id, uwi\.department_id\)|COALESCE\(camp\.department_id, uwi\.department_id\).*LEFT JOIN unofficial_whatsapp_campaigns camp ON camp\.id = NULLIF\(uwc\.campaign_id, ''\)::uuid`).
		WithArgs("conv-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"department_id"}).AddRow("dept-vendas"))

	got, err := NewConversationRepository(db).DepartmentIDForEntry(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "dept-vendas" {
		t.Fatalf("department = %q, want dept-vendas", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
