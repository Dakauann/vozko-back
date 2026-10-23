package emailinbox_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/conversation"
	"vozko/domain/emailinbox"
	"vozko/domain/export"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func emailDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "email_inbox", schema.EmailInboxModels()...)
	for _, ddl := range []string{
		`CREATE UNIQUE INDEX ux_email_contact_workspace_address ON email_contacts (workspace_id, address) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX ux_email_conversation_workspace_contact ON email_conversations (workspace_id, contact_id) WHERE deleted_at IS NULL`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("index: %v", err)
		}
	}
	return db
}

func TestInboxEnsureIsIdempotentAndScoped(t *testing.T) {
	db := emailDB(t)
	repo := NewInboxRepository(db)
	ctx := context.Background()
	ws, other, id := uuid.NewString(), uuid.NewString(), uuid.NewString()

	first, err := repo.Ensure(ctx, emailinbox.Inbox{ID: id, WorkspaceID: ws, Name: "Loja", Address: "vendas@loja.com.br"})
	if err != nil || first.Name != "Loja" {
		t.Fatalf("Ensure = %+v, %v", first, err)
	}
	dept := uuid.NewString()
	first.Apply(emailinbox.InboxSettings{DepartmentID: &dept, EnableAnalysis: true})
	if err := repo.Update(ctx, first); err != nil {
		t.Fatalf("Update: %v", err)
	}
	renamed, err := repo.Ensure(ctx, emailinbox.Inbox{ID: id, WorkspaceID: ws, Name: "Loja Nova", Address: "vendas@loja.com.br"})
	if err != nil || renamed.Name != "Loja Nova" || renamed.DepartmentID == nil || !renamed.EnableAnalysis {
		t.Fatalf("rename kept settings? %+v, %v", renamed, err)
	}
	if _, err := repo.Ensure(ctx, emailinbox.Inbox{ID: id, WorkspaceID: other, Name: "Intruso"}); !errors.Is(err, emailinbox.ErrInboxNotFound) {
		t.Fatalf("another workspace reached the inbox: %v", err)
	}
	if _, err := repo.FindByID(ctx, other, id); !errors.Is(err, emailinbox.ErrInboxNotFound) {
		t.Fatalf("scoped read crossed workspaces: %v", err)
	}
	list, err := repo.ListByWorkspace(ctx, ws)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListByWorkspace = %d, %v", len(list), err)
	}
}

func TestContactAndConversationFindOrCreate(t *testing.T) {
	db := emailDB(t)
	contacts, conversations := NewContactRepository(db), NewConversationRepository(db)
	ctx := context.Background()
	ws, sender := uuid.NewString(), uuid.NewString()

	c1, err := contacts.FindOrCreate(ctx, emailinbox.FindOrCreateContactInput{WorkspaceID: ws, Address: "ana@x.com", DisplayName: "Ana"})
	if err != nil {
		t.Fatalf("contact: %v", err)
	}
	c2, err := contacts.FindOrCreate(ctx, emailinbox.FindOrCreateContactInput{WorkspaceID: ws, Address: "ana@x.com", DisplayName: "Outra"})
	if err != nil || c2.ID != c1.ID || c2.DisplayName != "Ana" {
		t.Fatalf("second find = %+v, %v", c2, err)
	}
	lead := uuid.NewString()
	if err := contacts.LinkLead(ctx, c1.ID, lead); err != nil {
		t.Fatalf("LinkLead: %v", err)
	}
	if err := contacts.LinkLead(ctx, c1.ID, uuid.NewString()); err != nil {
		t.Fatalf("LinkLead again: %v", err)
	}
	linked, _ := contacts.FindByID(ctx, c1.ID)
	if linked.LeadID == nil || *linked.LeadID != lead {
		t.Fatalf("a linked lead must not be replaced: %+v", linked.LeadID)
	}

	conv, created, err := conversations.FindOrCreate(ctx, emailinbox.FindOrCreateConversationInput{WorkspaceID: ws, ContactID: c1.ID, SenderID: sender})
	if err != nil || !created {
		t.Fatalf("conversation = %v, created %v", err, created)
	}
	again, created, err := conversations.FindOrCreate(ctx, emailinbox.FindOrCreateConversationInput{WorkspaceID: ws, ContactID: c1.ID, SenderID: uuid.NewString()})
	if err != nil || created || again.ID != conv.ID || again.SenderID != sender {
		t.Fatalf("second conversation = %+v, created %v, %v", again, created, err)
	}

	thread := emailinbox.Thread{Subject: "Pedido", ThreadKey: "k@x", LastInboundMessageID: "m1@x", References: []string{"k@x", "m1@x"}}
	if err := conversations.ApplyThread(ctx, conv.ID, thread); err != nil {
		t.Fatalf("ApplyThread: %v", err)
	}
	at := time.Now().UTC()
	if err := conversations.RecordInbound(ctx, conv.ID, at); err != nil {
		t.Fatalf("RecordInbound: %v", err)
	}
	read, err := conversations.FindByID(ctx, conv.ID)
	if err != nil || read.Subject != "Pedido" || len(read.References) != 2 || read.LastCustomerMessageAt == nil {
		t.Fatalf("read back = %+v, %v", read, err)
	}
	if err := conversations.SetStatus(ctx, conv.ID, conversation.StatusWrite{Status: conversation.ConversationStatusFinished}); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	counts, err := conversations.CountByStatus(ctx, ws, "")
	if err != nil || counts[string(conversation.ConversationStatusFinished)] != 1 {
		t.Fatalf("counts = %v, %v", counts, err)
	}
	owner, err := conversations.WorkspaceIDForEntry(ctx, conv.ID)
	if err != nil || owner != ws {
		t.Fatalf("owner = %q, %v", owner, err)
	}
}

func TestDepartmentFollowsTheInbox(t *testing.T) {
	db := emailDB(t)
	inboxes, contacts, conversations := NewInboxRepository(db), NewContactRepository(db), NewConversationRepository(db)
	ctx := context.Background()
	ws, sender, dept := uuid.NewString(), uuid.NewString(), uuid.NewString()

	inbox, _ := inboxes.Ensure(ctx, emailinbox.Inbox{ID: sender, WorkspaceID: ws, Name: "Suporte"})
	contact, _ := contacts.FindOrCreate(ctx, emailinbox.FindOrCreateContactInput{WorkspaceID: ws, Address: "b@x.com"})
	conv, _, _ := conversations.FindOrCreate(ctx, emailinbox.FindOrCreateConversationInput{WorkspaceID: ws, ContactID: contact.ID, SenderID: sender})

	if got, err := conversations.DepartmentIDForEntry(ctx, conv.ID); err != nil || got != "" {
		t.Fatalf("no department yet = %q, %v", got, err)
	}
	inbox.Apply(emailinbox.InboxSettings{DepartmentID: &dept})
	if err := inboxes.Update(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	if got, err := conversations.DepartmentIDForEntry(ctx, conv.ID); err != nil || got != dept {
		t.Fatalf("department = %q, %v", got, err)
	}
}

func TestExportListsWorkspaceConversations(t *testing.T) {
	db := emailDB(t)
	contacts, conversations := NewContactRepository(db), NewConversationRepository(db)
	ctx := context.Background()
	ws, other, sender := uuid.NewString(), uuid.NewString(), uuid.NewString()

	mine, _ := contacts.FindOrCreate(ctx, emailinbox.FindOrCreateContactInput{WorkspaceID: ws, Address: "c@x.com", DisplayName: "Carla"})
	theirs, _ := contacts.FindOrCreate(ctx, emailinbox.FindOrCreateContactInput{WorkspaceID: other, Address: "c@x.com"})
	_, _, _ = conversations.FindOrCreate(ctx, emailinbox.FindOrCreateConversationInput{WorkspaceID: ws, ContactID: mine.ID, SenderID: sender})
	_, _, _ = conversations.FindOrCreate(ctx, emailinbox.FindOrCreateConversationInput{WorkspaceID: other, ContactID: theirs.ID, SenderID: sender})

	var rows []export.ChannelEntry
	err := NewExportRepository(db).ListForExport(ctx, export.Scope{WorkspaceID: ws}, func(e export.ChannelEntry) error {
		rows = append(rows, e)
		return nil
	})
	if err != nil || len(rows) != 1 || rows[0].Name != "Carla" || rows[0].Number != "c@x.com" || rows[0].Status != "new" {
		t.Fatalf("export = %+v, %v", rows, err)
	}
}
