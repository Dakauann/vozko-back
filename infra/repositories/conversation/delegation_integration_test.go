package conversation_repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

func delegationDB(t *testing.T) *gorm.DB {
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
	schemaName := "deleg_test_" + uuid.New().String()[:8]
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
	if err := db.AutoMigrate(&schema.ConversationDelegation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestDelegationsAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewDelegationRepository(delegationDB(t))
	ws := uuid.NewString()
	chat := shared.EntryRef{EntryID: uuid.NewString(), EntryType: shared.EntryTypeWebchat}
	other := shared.EntryRef{EntryID: chat.EntryID, EntryType: shared.EntryTypeTelegram}

	if got, err := repo.Find(ctx, chat.EntryID, chat.EntryType); err != nil || got != nil {
		t.Fatalf("no delegation yet = %+v %v", got, err)
	}

	agent := conversation.Automation{Kind: conversation.AutomationAgent, ID: "agent-1"}
	if err := repo.Save(ctx, conversation.Delegation{WorkspaceID: ws, EntryID: chat.EntryID, EntryType: chat.EntryType, Automation: agent, DelegatedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	workflow := conversation.Automation{Kind: conversation.AutomationWorkflow, ID: "wf-1"}
	if err := repo.Save(ctx, conversation.Delegation{WorkspaceID: ws, EntryID: chat.EntryID, EntryType: chat.EntryType, Automation: workflow, DelegatedBy: "ana"}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Find(ctx, chat.EntryID, chat.EntryType)
	if err != nil || got == nil || got.Automation != workflow || got.DelegatedBy != "ana" {
		t.Fatalf("a second delegation must replace the first: %+v %v", got, err)
	}

	many, err := repo.FindMany(ctx, []shared.EntryRef{chat, other})
	if err != nil || len(many) != 1 || many[chat] != workflow {
		t.Fatalf("FindMany = %+v %v, want only the webchat entry (same id on another channel is a different conversation)", many, err)
	}

	if err := repo.Delete(ctx, chat.EntryID, chat.EntryType); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Find(ctx, chat.EntryID, chat.EntryType); got != nil {
		t.Fatal("delegation survived Delete")
	}
}
