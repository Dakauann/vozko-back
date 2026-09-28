package unofficial_whatsapp_repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	uw "vozko/domain/unofficial_whatsapp"
)

func conversationColumns() []string {
	return []string{"id", "workspace_id", "instance_id", "contact_id", "chat_id", "campaign_id", "is_group", "created_at", "updated_at"}
}

func TestFindByChatID_TheNewestConversationIsTheChats(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "unofficial_whatsapp_conversations" WHERE \(instance_id = \$1 AND chat_id = \$2\).*ORDER BY created_at DESC`).
		WithArgs("inst-1", "5511@s.whatsapp.net", 1).
		WillReturnRows(sqlmock.NewRows(conversationColumns()).
			AddRow("conv-new", "ws-1", "inst-1", "contact-1", "5511@s.whatsapp.net", "camp-2", false, time.Now(), time.Now()))

	got, err := NewConversationRepository(db).FindByChatID(context.Background(), "inst-1", "5511@s.whatsapp.net")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "conv-new" || got.CampaignID != "camp-2" {
		t.Fatalf("got %+v, want the newest conversation with its campaign", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreate_ACampaignGetsItsOwnConversation(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "unofficial_whatsapp_conversations" WHERE \(instance_id = \$1 AND chat_id = \$2\) AND campaign_id = \$3`).
		WithArgs("inst-1", "5511@s.whatsapp.net", "camp-2", 1).
		WillReturnRows(sqlmock.NewRows(conversationColumns()))
	mock.ExpectQuery(`SELECT \* FROM "unofficial_whatsapp_conversations" WHERE \(instance_id = \$1 AND contact_id = \$2\) AND campaign_id = \$3`).
		WithArgs("inst-1", "contact-1", "camp-2", 1).
		WillReturnRows(sqlmock.NewRows(conversationColumns()))
	mock.ExpectExec(`INSERT INTO "unofficial_whatsapp_conversations" .*"campaign_id".*ON CONFLICT \("instance_id","chat_id","campaign_id"\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT \* FROM "unofficial_whatsapp_conversations" WHERE \(instance_id = \$1 AND chat_id = \$2\) AND campaign_id = \$3`).
		WithArgs("inst-1", "5511@s.whatsapp.net", "camp-2", 1).
		WillReturnRows(sqlmock.NewRows(conversationColumns()).
			AddRow("conv-camp-2", "ws-1", "inst-1", "contact-1", "5511@s.whatsapp.net", "camp-2", false, time.Now(), time.Now()))

	got, err := NewConversationRepository(db).FindOrCreate(context.Background(), uw.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-1", ContactID: "contact-1", ChatID: "5511@s.whatsapp.net", CampaignID: "camp-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "conv-camp-2" {
		t.Fatalf("got %q, want the campaign's own conversation", got.ID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOrCreate_WithoutACampaignKeepsTheChatsCurrentConversation(t *testing.T) {
	db, mock, sqlDB := newLookupDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "unofficial_whatsapp_conversations" WHERE \(instance_id = \$1 AND chat_id = \$2\).*ORDER BY created_at DESC`).
		WithArgs("inst-1", "5511@s.whatsapp.net", 1).
		WillReturnRows(sqlmock.NewRows(conversationColumns()).
			AddRow("conv-camp-2", "ws-1", "inst-1", "contact-1", "5511@s.whatsapp.net", "camp-2", false, time.Now(), time.Now()))

	got, err := NewConversationRepository(db).FindOrCreate(context.Background(), uw.FindOrCreateConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-1", ContactID: "contact-1", ChatID: "5511@s.whatsapp.net",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "conv-camp-2" {
		t.Fatalf("got %q, want the chat's newest conversation", got.ID)
	}
}
