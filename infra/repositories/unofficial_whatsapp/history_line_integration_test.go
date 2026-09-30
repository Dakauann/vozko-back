package unofficial_whatsapp_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/database/schema"
)

func historyIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	return postgresTestDB(t, []any{&schema.UnofficialWhatsAppHistorySync{}}, []string{
		`CREATE UNIQUE INDEX ux_uw_history_active_per_instance
			ON unofficial_whatsapp_history_syncs (instance_id)
			WHERE status IN ('QUEUED', 'RUNNING')`,
	})
}

func queuedSync(t *testing.T, instanceID string, now time.Time) *uw.HistorySync {
	t.Helper()
	run, err := uw.NewHistorySync(&uw.Instance{ID: instanceID, WorkspaceID: uuid.NewString()},
		uw.HistorySyncTriggerConnect, now, uw.DefaultHistorySyncPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestHistorySyncOneActiveRunPerInstanceAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewHistorySyncRepository(historyIntegrationDB(t))
	instanceID := uuid.NewString()
	now := time.Now().UTC()

	if err := repo.Create(ctx, queuedSync(t, instanceID, now)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := repo.Create(ctx, queuedSync(t, instanceID, now)); !errors.Is(err, uw.ErrHistorySyncActive) {
		t.Fatalf("second active run: err = %v, want ErrHistorySyncActive", err)
	}
	if err := repo.Create(ctx, queuedSync(t, uuid.NewString(), now)); err != nil {
		t.Fatalf("another instance must not be blocked: %v", err)
	}
}

func TestHistorySyncClaimLeaseAndSaveAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewHistorySyncRepository(historyIntegrationDB(t))
	now := time.Now().UTC()
	run := queuedSync(t, uuid.NewString(), now)
	if err := repo.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimDue(ctx, now.Add(time.Second), "worker-a", now.Add(20*time.Minute), 5)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: %v, %d rows", err, len(claimed))
	}
	again, err := repo.ClaimDue(ctx, now.Add(2*time.Second), "worker-b", now.Add(20*time.Minute), 5)
	if err != nil || len(again) != 0 {
		t.Fatalf("a leased run was claimed twice: %v, %d rows", err, len(again))
	}

	got := claimed[0]
	if got.Status != uw.HistorySyncRunning || got.LeaseOwner != "worker-a" || got.StartedAt == nil {
		t.Fatalf("claimed run = %+v", got)
	}

	stale := *got
	stale.LeaseOwner = "worker-b"
	if err := repo.Save(ctx, &stale); !errors.Is(err, uw.ErrHistorySyncLeaseLost) {
		t.Errorf("a worker without the lease saved: err = %v", err)
	}

	got.CompletePass(uw.HistoryPass{Seen: 3, Imported: 3}, now.Add(time.Minute), uw.DefaultHistorySyncPolicy())
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("save: %v", err)
	}
	latest, err := repo.FindLatest(ctx, got.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.MessagesImported != 3 || latest.LeaseOwner != "" || latest.Passes != 1 {
		t.Errorf("saved run = %+v", latest)
	}
}

func TestHistorySyncExpiredLeaseIsReclaimedAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewHistorySyncRepository(historyIntegrationDB(t))
	now := time.Now().UTC()
	run := queuedSync(t, uuid.NewString(), now)
	if err := repo.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimDue(ctx, now, "dead-worker", now.Add(time.Minute), 1); err != nil {
		t.Fatal(err)
	}

	reclaimed, err := repo.ClaimDue(ctx, now.Add(2*time.Minute), "worker-b", now.Add(22*time.Minute), 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].LeaseOwner != "worker-b" {
		t.Fatalf("expired lease not reclaimed: %v, %+v", err, reclaimed)
	}
}

func TestHistorySyncResumePullsTheNextPollForwardAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewHistorySyncRepository(historyIntegrationDB(t))
	now := time.Now().UTC()
	run := queuedSync(t, uuid.NewString(), now)
	run.NextPollAt = now.Add(time.Hour)
	if err := repo.Create(ctx, run); err != nil {
		t.Fatal(err)
	}

	if err := repo.Resume(ctx, run.ID, now, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	active, err := repo.FindActive(ctx, run.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if active.NextPollAt.After(now.Add(time.Second)) || active.PollUntil.Before(now.Add(3*time.Hour).Add(-time.Second)) {
		t.Errorf("resume did not take: next=%v until=%v", active.NextPollAt, active.PollUntil)
	}
}

func lineIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	return postgresTestDB(t,
		[]any{
			&schema.UnofficialWhatsAppContact{},
			&schema.UnofficialWhatsAppConversation{},
			&schema.UnofficialWhatsAppGroup{},
			&schema.UnofficialWhatsAppGroupParticipant{},
		},
		append([]string{
			`CREATE UNIQUE INDEX ux_uw_contact_instance_jid
				ON unofficial_whatsapp_contacts (instance_id, jid) WHERE deleted_at IS NULL`,
			`CREATE UNIQUE INDEX ux_uw_contact_instance_lid
				ON unofficial_whatsapp_contacts (instance_id, lid) WHERE lid <> '' AND deleted_at IS NULL`,
			`CREATE UNIQUE INDEX ux_uw_group_instance_jid
				ON unofficial_whatsapp_groups (instance_id, jid) WHERE deleted_at IS NULL`,
		}, conversationIndexes...),
	)
}

type lineFixture struct {
	db        *gorm.DB
	workspace string
	old, new  string
}

func (f *lineFixture) contact(t *testing.T, instanceID, jid string) string {
	t.Helper()
	row := &schema.UnofficialWhatsAppContact{WorkspaceID: f.workspace, InstanceID: instanceID, JID: jid}
	if err := f.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func (f *lineFixture) conversation(t *testing.T, instanceID, contactID, chatID string) string {
	t.Helper()
	row := &schema.UnofficialWhatsAppConversation{
		ID: uuid.NewString(), WorkspaceID: f.workspace, InstanceID: instanceID, ContactID: contactID, ChatID: chatID,
	}
	if err := f.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func (f *lineFixture) instanceOf(t *testing.T, table, id string) string {
	t.Helper()
	var instanceID string
	if err := f.db.Table(table).Where("id = ?", id).Pluck("instance_id", &instanceID).Error; err != nil {
		t.Fatal(err)
	}
	return instanceID
}

func newLineFixture(t *testing.T) *lineFixture {
	return &lineFixture{db: lineIntegrationDB(t), workspace: uuid.NewString(), old: uuid.NewString(), new: uuid.NewString()}
}

func TestLineTransferMovesConversationsAndContactsAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	f := newLineFixture(t)
	ana := f.contact(t, f.old, "5511111111111@s.whatsapp.net")
	anaConv := f.conversation(t, f.old, ana, "5511111111111@s.whatsapp.net")
	group := &schema.UnofficialWhatsAppGroup{WorkspaceID: f.workspace, InstanceID: f.old, JID: "120363@g.us"}
	if err := f.db.Create(group).Error; err != nil {
		t.Fatal(err)
	}

	got, err := NewLineRepository(f.db).Transfer(ctx, f.old, f.new)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if got.Contacts != 1 || got.Conversations != 1 || got.Groups != 1 || got.ConversationsKept != 0 {
		t.Errorf("transfer = %+v", got)
	}
	if f.instanceOf(t, "unofficial_whatsapp_conversations", anaConv) != f.new {
		t.Error("the conversation id must survive and now belong to the new number, so its messages follow it")
	}
	if f.instanceOf(t, "unofficial_whatsapp_contacts", ana) != f.new {
		t.Error("the contact must follow its conversation")
	}
}

func TestLineTransferMergesIntoAContactTheNewNumberAlreadyHasAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	f := newLineFixture(t)
	jid := "5511222222222@s.whatsapp.net"
	oldBruno := f.contact(t, f.old, jid)
	newBruno := f.contact(t, f.new, jid)
	conv := f.conversation(t, f.old, oldBruno, jid)

	got, err := NewLineRepository(f.db).Transfer(ctx, f.old, f.new)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if got.ContactsMerged != 1 || got.Conversations != 1 {
		t.Errorf("transfer = %+v", got)
	}
	var contactID string
	f.db.Table("unofficial_whatsapp_conversations").Where("id = ?", conv).Pluck("contact_id", &contactID)
	if contactID != newBruno {
		t.Errorf("conversation contact = %s, want the new number's contact %s", contactID, newBruno)
	}
}

func TestLineTransferKeepsAChatTheNewNumberAlreadyStartedAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	f := newLineFixture(t)
	jid := "5511333333333@s.whatsapp.net"
	oldCarla := f.contact(t, f.old, jid)
	oldConv := f.conversation(t, f.old, oldCarla, jid)
	newCarla := f.contact(t, f.new, jid)
	f.conversation(t, f.new, newCarla, jid)

	got, err := NewLineRepository(f.db).Transfer(ctx, f.old, f.new)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}

	if got.Conversations != 0 || got.ConversationsKept != 1 {
		t.Errorf("transfer = %+v", got)
	}
	if f.instanceOf(t, "unofficial_whatsapp_conversations", oldConv) != f.old {
		t.Error("a colliding conversation must stay where it was rather than break the unique index")
	}
}

func TestLineTransferWorksInBatchesAndIsIdempotentAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	f := newLineFixture(t)
	for i := 0; i < 7; i++ {
		jid := uuid.NewString()[:12] + "@s.whatsapp.net"
		f.conversation(t, f.old, f.contact(t, f.old, jid), jid)
	}
	repo := &lineRepository{db: f.db, batch: 3}

	first, err := repo.Transfer(ctx, f.old, f.new)
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	second, err := repo.Transfer(ctx, f.old, f.new)
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}

	if first.Contacts != 7 || first.Conversations != 7 {
		t.Errorf("first = %+v", first)
	}
	if second.Moved() {
		t.Errorf("a repeated transfer moved rows again: %+v", second)
	}
}
