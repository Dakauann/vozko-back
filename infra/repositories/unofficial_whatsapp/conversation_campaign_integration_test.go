package unofficial_whatsapp_repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

func conversationIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "uwc_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&schema.UnofficialWhatsAppConversation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The production unique keys (infra/database/indexes.go): the upsert's
	// ON CONFLICT target must match them exactly or Postgres rejects the insert.
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX ux_uw_conversation_instance_contact_campaign
			ON unofficial_whatsapp_conversations (instance_id, contact_id, campaign_id)
			WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX ux_uw_conversation_instance_chat_campaign
			ON unofficial_whatsapp_conversations (instance_id, chat_id, campaign_id)
			WHERE chat_id <> '' AND deleted_at IS NULL`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index: %v", err)
		}
	}
	return db
}

func TestConversationsPerCampaignAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewConversationRepository(conversationIntegrationDB(t))
	base := uw.FindOrCreateConversationInput{
		WorkspaceID: uuid.NewString(),
		InstanceID:  uuid.NewString(),
		ContactID:   uuid.NewString(),
		ChatID:      "5511999990000@s.whatsapp.net",
	}
	promo, retomada := uuid.NewString(), uuid.NewString()

	// Ana writes first, with no campaign: a campaign-less conversation.
	walkIn, err := repo.FindOrCreate(ctx, base)
	if err != nil {
		t.Fatal(err)
	}

	// Month 1 and month 2 campaigns each open their own conversation.
	withCampaign := func(id string) uw.FindOrCreateConversationInput { in := base; in.CampaignID = id; return in }
	month1, err := repo.FindOrCreate(ctx, withCampaign(promo))
	if err != nil {
		t.Fatal(err)
	}
	month2, err := repo.FindOrCreate(ctx, withCampaign(retomada))
	if err != nil {
		t.Fatal(err)
	}
	if walkIn.ID == month1.ID || month1.ID == month2.ID || walkIn.ID == month2.ID {
		t.Fatalf("want three conversations, got %s %s %s", walkIn.ID, month1.ID, month2.ID)
	}
	if month2.CampaignID != retomada {
		t.Fatalf("month 2 campaign = %q, want %q", month2.CampaignID, retomada)
	}

	// The next wave of the same campaign reuses its conversation.
	again, err := repo.FindOrCreate(ctx, withCampaign(retomada))
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != month2.ID {
		t.Fatalf("a resend opened %s, want the campaign's %s", again.ID, month2.ID)
	}

	// Ana's reply, or anything that is not a campaign send, goes to the newest.
	current, err := repo.FindByChatID(ctx, base.InstanceID, base.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != month2.ID {
		t.Fatalf("current conversation = %s, want the newest %s", current.ID, month2.ID)
	}
	inbound, err := repo.FindOrCreate(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if inbound.ID != month2.ID {
		t.Fatalf("a campaign-less message went to %s, want the newest %s", inbound.ID, month2.ID)
	}
}
