package entrypurge_repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func purgeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "entrypurge_test")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(
		&schema.ConversationMessage{}, &schema.ConversationMedia{}, &schema.ConversationEvent{}, &schema.ConversationNote{},
		&schema.ConversationPersonalMark{}, &schema.ConversationDelegation{}, &schema.ConversationAdOrigin{},
		&schema.ConversationLiveRead{}, &schema.LiveDecisionRecord{}, &schema.AIAttendanceSession{}, &schema.AssignmentHistory{},
		&schema.InboxAssignment{}, &schema.EntryLabel{}, &schema.EntryStage{}, &schema.OpportunityConversation{},
		&schema.ScheduledMessage{}, &schema.LeadExitRun{}, &schema.WhatsAppFormSend{}, &schema.WhatsAppFormResponse{},
		&schema.LeadMemory{}, &schema.WorkflowRunSchema{}, &schema.WorkflowRunLogSchema{}, &schema.CustomerPortfolioEntry{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func count(t *testing.T, db *gorm.DB, table, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestPurgingEntriesRemovesTheirRowsInEveryTableAndNothingElse(t *testing.T) {
	db := purgeDB(t)
	ctx := context.Background()
	target, kept, otherChannel := uuid.NewString(), uuid.NewString(), uuid.NewString()
	ws := uuid.NewString()
	seed := func(entryID, entryType string) {
		for _, row := range []any{
			&schema.ConversationMessage{EntryID: entryID, EntryType: entryType},
			&schema.ConversationMedia{EntryID: entryID, EntryType: entryType, Type: "image", URL: "https://files.example/" + entryID + ".jpg"},
			&schema.EntryLabel{ID: uuid.NewString(), LabelID: uuid.NewString(), EntryID: entryID, EntryType: entryType, WorkspaceID: ws},
		} {
			if err := db.Omit(clause.Associations).Create(row).Error; err != nil {
				t.Fatalf("seed %T: %v", row, err)
			}
		}
		run := &schema.WorkflowRunSchema{WorkflowID: uuid.NewString(), WorkspaceID: ws, EntryID: entryID, EntryType: entryType, State: "{}"}
		if err := db.Omit(clause.Associations).Create(run).Error; err != nil {
			t.Fatal(err)
		}
		emptyPayload := "{}"
		if err := db.Omit(clause.Associations).Create(&schema.WorkflowRunLogSchema{RunID: run.ID, NodeID: "n", NodeType: "t", Status: "ok", Input: &emptyPayload, Output: &emptyPayload}).Error; err != nil {
			t.Fatal(err)
		}
		source := entryID
		if err := db.Omit(clause.Associations).Create(&schema.LeadMemory{WorkspaceID: ws, LeadID: uuid.NewString(), Category: "c", Content: "x", ContentNorm: "x",
			ActorKind: "human", ActorID: "u", SourceEntryID: &source, SourceEntryType: &entryType}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seed(target, "mercadolivre")
	seed(kept, "mercadolivre")
	seed(otherChannel, "instagram")
	if err := db.Exec("UPDATE workflow_runs SET entry_id = ? WHERE entry_type = ?", target, "instagram").Error; err != nil {
		t.Fatal(err)
	}

	s := NewStore(db)
	urls, err := s.MediaURLs(ctx, shared.EntryTypeMercadoLivre, []string{target})
	if err != nil || len(urls) != 1 || urls[0] != "https://files.example/"+target+".jpg" {
		t.Fatalf("media urls = %v %v", urls, err)
	}
	if err := s.DeleteEntries(ctx, shared.EntryTypeMercadoLivre, []string{target}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"conversation_messages", "conversation_media", "entry_labels", "workflow_runs"} {
		if n := count(t, db, table, "entry_id = ? AND entry_type = ?", target, "mercadolivre"); n != 0 {
			t.Errorf("%s kept %d rows of the purged entry", table, n)
		}
		if n := count(t, db, table, "entry_id = ?", kept); n != 1 {
			t.Errorf("%s lost a row of an entry that was not purged (%d)", table, n)
		}
	}
	if n := count(t, db, "lead_memories", "source_entry_id = ? AND source_entry_type = ?", target, "mercadolivre"); n != 0 {
		t.Errorf("lead memories kept %d rows", n)
	}
	if n := count(t, db, "workflow_runs", "entry_type = ?", "instagram"); n != 1 {
		t.Errorf("a same-id entry of another channel was touched (%d)", n)
	}
	if n := count(t, db, "workflow_run_logs", "1 = 1"); n != 2 {
		t.Errorf("run logs left = %d, want the two of the runs that stayed", n)
	}

	contact, otherContact := uuid.NewString(), uuid.NewString()
	for _, row := range []*schema.CustomerPortfolioEntry{
		{ID: uuid.NewString(), WorkspaceID: ws, HolderID: uuid.NewString(), ContactType: "mercadolivre", ContactID: schema.OptionalText(contact)},
		{ID: uuid.NewString(), WorkspaceID: ws, HolderID: uuid.NewString(), ContactType: "mercadolivre", ContactID: schema.OptionalText(otherContact)},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteContacts(ctx, shared.EntryTypeMercadoLivre, []string{contact}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, "customer_portfolio_entries", "1 = 1"); n != 1 {
		t.Fatalf("portfolio rows = %d", n)
	}
}
