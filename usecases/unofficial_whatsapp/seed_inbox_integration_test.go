package unofficial_whatsapp_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	conversation_repository "vozko/infra/repositories/conversation"
	lead_repository "vozko/infra/repositories/lead"
	uwrepo "vozko/infra/repositories/unofficial_whatsapp"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

// The claim this feature rests on cannot be checked with a fake: that a seeded
// conversation is VISIBLE in the inbox. Visibility is decided by two SQL gates
// in infra (the union's `last_message_at IS NOT NULL` and the hydration's
// inner JOIN LATERAL over conversation_messages), and no in-memory double
// models either of them.
//
// So this runs the real use case, through the real repositories, against a real
// Postgres, and then asks the production inbox query whether the conversation
// it created shows up. Everything happens inside a transaction that is always
// rolled back, so the development database is untouched.
//
// Opt-in: set VOZKO_TEST_DB=1 and the DB_* variables the application reads.

func integrationTx(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	// The instance row carries an encrypted provider token, so reading one at
	// all needs the PII service the container installs at startup. Same keys
	// from the same environment, or the rows do not decrypt.
	piiSvc, err := pii.LoadFromEnv()
	if err != nil {
		t.Skipf("PII encryption is not configured in this environment: %v", err)
	}
	piigorm.SetService(piiSvc)
	t.Cleanup(func() { piigorm.SetService(nil) })

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	t.Cleanup(func() {
		tx.Rollback()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return tx
}

// anyConnectedInstance finds a workspace that has a live number to seed onto.
// The fixture is whatever the development database happens to hold, so the test
// skips rather than fails when there is none.
func anyConnectedInstance(t *testing.T, tx *gorm.DB) (workspaceID, instanceID string) {
	t.Helper()
	var row struct {
		WorkspaceID string `gorm:"column:workspace_id"`
		ID          string `gorm:"column:id"`
	}
	err := tx.Raw(`SELECT workspace_id::text, id::text
	               FROM unofficial_whatsapp_instances
	               WHERE status = 'CONNECTED' AND deleted_at IS NULL
	               ORDER BY created_at ASC LIMIT 1`).Scan(&row).Error
	if err != nil || row.ID == "" {
		t.Skip("no connected unofficial whatsapp instance in this database")
	}
	return row.WorkspaceID, row.ID
}

// visibleInInbox reproduces the two production gates, verbatim in shape:
// entry_sources.go's union predicate, and message_repository.go's hydration
// LATERAL. If a seeded conversation survives both, it renders in the CRM.
func visibleInInbox(t *testing.T, tx *gorm.DB, workspaceID, conversationID string) bool {
	t.Helper()
	var found int64
	err := tx.Raw(`
		WITH all_entries AS (
			SELECT uwc.id AS entry_id, uwc.last_message_at AS lm_created_at
			FROM unofficial_whatsapp_conversations uwc
			JOIN unofficial_whatsapp_instances uwi
			  ON uwi.id = uwc.instance_id AND uwi.workspace_id = ?
			WHERE uwc.deleted_at IS NULL
			  AND uwc.last_message_at IS NOT NULL
			  AND uwc.conversation_status IS DISTINCT FROM 'finished'
		)
		SELECT COUNT(*)
		FROM all_entries ae
		JOIN LATERAL (
			SELECT cm.created_at
			FROM conversation_messages cm
			WHERE cm.entry_id = ae.entry_id
			  AND cm.entry_type = 'unofficial_whatsapp'
			  AND cm.deleted_at IS NULL
			ORDER BY cm.created_at DESC, cm.id DESC LIMIT 1
		) lm ON true
		WHERE ae.entry_id = ?`, workspaceID, conversationID).Scan(&found).Error
	if err != nil {
		t.Fatalf("inbox visibility query: %v", err)
	}
	return found == 1
}

func newIntegrationSeeder(tx *gorm.DB) *uwuc.SeedInboxUseCase {
	return newIntegrationSeederWith(tx, nil)
}

// newIntegrationSeederWith is the same seeder with a scripter attached.
//
// The scripter is a FAKE even here, and deliberately: this test is about what
// Postgres and the production inbox queries do with a written thread, not about
// what a model writes. Calling a real provider would make it slow, non
// deterministic and billable, and would be a test of OpenRouter rather than of
// us.
func newIntegrationSeederWith(tx *gorm.DB, scripter uw.ConversationScripter) *uwuc.SeedInboxUseCase {
	return uwuc.NewSeedInboxUseCase(
		uwrepo.NewInstanceRepository(tx),
		uwrepo.NewContactRepository(tx),
		uwrepo.NewConversationRepository(tx),
		uwrepo.NewLeadLinker(lead_repository.NewRepository(tx)),
		conversation_repository.NewRepository(tx),
		scripter,
		// No balance checker: a nil one allows, and the floor has its own unit
		// test. Wiring a real one here would make this skip on any database
		// whose workspaces happen to sit at zero.
		nil,
	)
}

// scriptedFake answers every subject with the same four-message shape the
// dialog's default asks for, ending on the lead's turn.
type scriptedFake struct{}

func (scriptedFake) Script(_ context.Context, req uw.ScriptRequest) (*uw.ScriptResult, error) {
	threads := make([]uw.ScriptedThread, 0, len(req.Subjects))
	for _, subject := range req.Subjects {
		threads = append(threads, uw.ScriptedThread{
			Ref: subject.Ref,
			Turns: []uw.ScriptTurn{
				{FromLead: true, Text: "oi! vi sim, queria saber sobre o valor"},
				{FromLead: false, Text: "Claro. Posso te passar as condicoes por aqui mesmo?"},
				{FromLead: true, Text: "pode sim, to olhando agora"},
			},
		})
	}
	return &uw.ScriptResult{Threads: threads, Model: "fake", FinishReason: "stop"}, nil
}

type failingScripter struct{}

func (failingScripter) Script(context.Context, uw.ScriptRequest) (*uw.ScriptResult, error) {
	return nil, errors.New("upstream refused")
}

func TestSeedInboxMakesTheConversationVisibleInTheInbox(t *testing.T) {
	tx := integrationTx(t)
	workspaceID, _ := anyConnectedInstance(t, tx)

	// A number this database has certainly never seen, so the run exercises
	// creation rather than reconciliation.
	const number = "5511900000001"
	seeder := newIntegrationSeeder(tx)

	out, err := seeder.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: workspaceID,
		Targets:     []uw.SeedTarget{{Number: number, Name: "Integração Vozko"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 {
		t.Fatalf("outcome = %+v, want 1 seeded", out)
	}

	var conversationID string
	if err := tx.Raw(`
		SELECT uwc.id::text
		FROM unofficial_whatsapp_conversations uwc
		JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwc.contact_id
		WHERE uwct.phone_number = ? AND uwc.workspace_id = ?
		ORDER BY uwc.created_at DESC LIMIT 1`, number, workspaceID).
		Scan(&conversationID).Error; err != nil {
		t.Fatalf("locate seeded conversation: %v", err)
	}
	if conversationID == "" {
		t.Fatal("no conversation was created for the seeded number")
	}

	// The whole point.
	if !visibleInInbox(t, tx, workspaceID, conversationID) {
		t.Fatal("the seeded conversation does not pass the production inbox gates")
	}

	// It must be visible WITHOUT looking like a message the lead sent.
	var inbound int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages
		WHERE entry_id = ? AND entry_type = 'unofficial_whatsapp' AND deleted_at IS NULL
		  AND message_type IN ('user_message','audio','media','story_reply','story_mention','post_share')`,
		conversationID).Scan(&inbound).Error; err != nil {
		t.Fatalf("inbound count: %v", err)
	}
	if inbound != 0 {
		t.Fatalf("seeding wrote %d inbound messages; it must write none", inbound)
	}

	// And the lead bridge has to have happened, or the inbox row renders a bare
	// number where every other conversation renders a name.
	var leadID string
	if err := tx.Raw(`
		SELECT COALESCE(uwct.lead_id::text, '')
		FROM unofficial_whatsapp_contacts uwct
		WHERE uwct.phone_number = ? AND uwct.workspace_id = ?
		ORDER BY uwct.created_at DESC LIMIT 1`, number, workspaceID).
		Scan(&leadID).Error; err != nil {
		t.Fatalf("lead bridge lookup: %v", err)
	}
	if leadID == "" {
		t.Fatal("the seeded contact was not bridged to a CRM lead")
	}
}

// Running the same import twice must not write a second placeholder, and must
// not move the conversation in the inbox.
func TestSeedInboxIsIdempotentAgainstPostgres(t *testing.T) {
	tx := integrationTx(t)
	workspaceID, _ := anyConnectedInstance(t, tx)

	const number = "5511900000002"
	seeder := newIntegrationSeeder(tx)
	req := uw.SeedRequest{WorkspaceID: workspaceID, Targets: []uw.SeedTarget{{Number: number}}}

	if _, err := seeder.Execute(context.Background(), req); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	out, err := seeder.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if out.Seeded != 0 || out.AlreadyActive != 1 {
		t.Fatalf("second run outcome = %+v, want 1 already active", out)
	}

	var messages int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages cm
		JOIN unofficial_whatsapp_conversations uwc ON uwc.id = cm.entry_id
		JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwc.contact_id
		WHERE uwct.phone_number = ? AND cm.entry_type = 'unofficial_whatsapp'
		  AND cm.deleted_at IS NULL`, number).Scan(&messages).Error; err != nil {
		t.Fatalf("message count: %v", err)
	}
	if messages != 1 {
		t.Fatalf("two runs left %d messages, want 1", messages)
	}
}

// A conversation that already carries real history must come out untouched:
// same message count, same last_message_at, no blank placeholder appended.
func TestSeedInboxLeavesALiveConversationAlone(t *testing.T) {
	tx := integrationTx(t)

	var row struct {
		WorkspaceID string `gorm:"column:workspace_id"`
		ConvID      string `gorm:"column:id"`
		Phone       string `gorm:"column:phone_number"`
	}
	err := tx.Raw(`
		SELECT uwc.workspace_id::text, uwc.id::text, uwct.phone_number
		FROM unofficial_whatsapp_conversations uwc
		JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwc.contact_id
		JOIN unofficial_whatsapp_instances uwi
		  ON uwi.id = uwc.instance_id AND uwi.status = 'CONNECTED'
		WHERE uwc.deleted_at IS NULL
		  AND uwc.last_message_at IS NOT NULL
		  AND uwct.phone_number <> ''
		  AND uwct.is_group = false
		ORDER BY uwc.last_message_at DESC LIMIT 1`).Scan(&row).Error
	if err != nil || row.ConvID == "" {
		t.Skip("no live unofficial whatsapp conversation in this database")
	}

	before := entryState(t, tx, row.ConvID)

	out, execErr := newIntegrationSeeder(tx).Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: row.WorkspaceID,
		Targets:     []uw.SeedTarget{{Number: row.Phone}},
	})
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if out.AlreadyActive != 1 {
		t.Fatalf("outcome = %+v, want 1 already active", out)
	}

	after := entryState(t, tx, row.ConvID)
	if after.messages != before.messages {
		t.Errorf("message count moved from %d to %d", before.messages, after.messages)
	}
	if after.lastMessageAt != before.lastMessageAt {
		t.Errorf("last_message_at moved from %q to %q", before.lastMessageAt, after.lastMessageAt)
	}
}

type entrySnapshot struct {
	messages      int64
	lastMessageAt string
}

func entryState(t *testing.T, tx *gorm.DB, conversationID string) entrySnapshot {
	t.Helper()
	var snap entrySnapshot
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages
		WHERE entry_id = ? AND entry_type = ? AND deleted_at IS NULL`,
		conversationID, string(shared.EntryTypeUnofficialWhatsApp)).Scan(&snap.messages).Error; err != nil {
		t.Fatalf("snapshot messages: %v", err)
	}
	if err := tx.Raw(`
		SELECT COALESCE(last_message_at::text, '')
		FROM unofficial_whatsapp_conversations WHERE id = ?`,
		conversationID).Scan(&snap.lastMessageAt).Error; err != nil {
		t.Fatalf("snapshot last_message_at: %v", err)
	}
	return snap
}

// ---- scripted seeding ----

// inboxPreview reproduces the hydration LATERAL the inbox list uses to pick the
// message shown under each row, and the unread predicate beside it.
//
// Both live in infra, in SQL, and no in-memory double models either. A scripted
// thread that wrote four rows but renders the operator's own opening as the
// preview would look, in the product, like a conversation nobody answered.
func inboxPreview(t *testing.T, tx *gorm.DB, conversationID string) (string, int64) {
	t.Helper()
	var text string
	if err := tx.Raw(`
		SELECT cm.text
		FROM conversation_messages cm
		WHERE cm.entry_id = ? AND cm.entry_type = 'unofficial_whatsapp'
		  AND cm.deleted_at IS NULL
		ORDER BY cm.created_at DESC, cm.id DESC LIMIT 1`, conversationID).
		Scan(&text).Error; err != nil {
		t.Fatalf("inbox preview: %v", err)
	}
	var unread int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages cm
		WHERE cm.entry_id = ? AND cm.entry_type = 'unofficial_whatsapp'
		  AND cm.read = false AND cm.deleted_at IS NULL
		  AND cm.message_type IN ('user_message','audio','media','story_reply','story_mention','post_share')`,
		conversationID).Scan(&unread).Error; err != nil {
		t.Fatalf("unread count: %v", err)
	}
	return text, unread
}

// locateSeededConversation finds the conversation a number was seeded into.
func locateSeededConversation(t *testing.T, tx *gorm.DB, workspaceID, number string) string {
	t.Helper()
	var conversationID string
	if err := tx.Raw(`
		SELECT uwc.id::text
		FROM unofficial_whatsapp_conversations uwc
		JOIN unofficial_whatsapp_contacts uwct ON uwct.id = uwc.contact_id
		WHERE uwct.phone_number = ? AND uwc.workspace_id = ?
		ORDER BY uwc.created_at DESC LIMIT 1`, number, workspaceID).
		Scan(&conversationID).Error; err != nil {
		t.Fatalf("locate seeded conversation: %v", err)
	}
	if conversationID == "" {
		t.Fatal("no conversation was created for the seeded number")
	}
	return conversationID
}

func countSeededMessages(t *testing.T, tx *gorm.DB, conversationID string) int64 {
	t.Helper()
	var messages int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages
		WHERE entry_id = ? AND entry_type = 'unofficial_whatsapp' AND deleted_at IS NULL`,
		conversationID).Scan(&messages).Error; err != nil {
		t.Fatalf("message count: %v", err)
	}
	return messages
}

// The claim the whole feature rests on, checked against the real queries: a
// scripted conversation is visible, reads as a conversation, and carries
// exactly one unread message because it ends on the lead's turn.
func TestSeedInboxScriptedConversationIsVisibleAndReadsAsAThread(t *testing.T) {
	tx := integrationTx(t)
	workspaceID, _ := anyConnectedInstance(t, tx)

	const number = "5511900000003"
	seeder := newIntegrationSeederWith(tx, scriptedFake{})

	out, err := seeder.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: workspaceID,
		Targets:     []uw.SeedTarget{{Number: number, Name: "Integracao Vozko"}},
		Script: &uw.SeedScript{
			Bodies:      []string{"Oi {{1}}, tudo bem? Vi que voce se interessou no curso."},
			MaxMessages: 4,
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 1 {
		t.Fatalf("outcome = %+v, want 1 seeded and 1 scripted", out)
	}

	conversationID := locateSeededConversation(t, tx, workspaceID, number)

	// Gate one and gate two, the same pair the blank placeholder has to pass.
	if !visibleInInbox(t, tx, workspaceID, conversationID) {
		t.Fatal("the scripted conversation does not pass the production inbox gates")
	}
	if got := countSeededMessages(t, tx, conversationID); got != 4 {
		t.Fatalf("wrote %d messages, want the 4-message thread", got)
	}

	// The preview is the LEAD's last line, not ours. This is what an operator
	// actually sees in the list, and getting it wrong makes a seeded inbox look
	// like a list of messages nobody replied to.
	preview, unread := inboxPreview(t, tx, conversationID)
	if preview != "pode sim, to olhando agora" {
		t.Errorf("inbox preview = %q, want the lead's last line", preview)
	}
	// Exactly one: the trailing inbound message. The three before it are read.
	if unread != 1 {
		t.Errorf("unread_count = %d, want 1", unread)
	}

	// last_message_at has to land on the newest turn, or the conversation sinks
	// in a list sorted by it. touchEntryMessageClocks is monotonic and the
	// thread is written in order, which is what makes backdating safe.
	var lastMessageAt, newestMessage string
	if err := tx.Raw(`SELECT COALESCE(last_message_at::text, '')
	                  FROM unofficial_whatsapp_conversations WHERE id = ?`,
		conversationID).Scan(&lastMessageAt).Error; err != nil {
		t.Fatalf("last_message_at: %v", err)
	}
	if err := tx.Raw(`SELECT COALESCE(MAX(created_at)::text, '')
	                  FROM conversation_messages
	                  WHERE entry_id = ? AND entry_type = 'unofficial_whatsapp' AND deleted_at IS NULL`,
		conversationID).Scan(&newestMessage).Error; err != nil {
		t.Fatalf("newest message: %v", err)
	}
	if lastMessageAt != newestMessage {
		t.Errorf("last_message_at = %q, want the newest message at %q", lastMessageAt, newestMessage)
	}

	// Ending on the lead's turn sets last_customer_message_at, which puts the
	// thread into the ordinary idle auto-close window. Correct, and worth
	// pinning before someone reports seeded conversations "closing themselves".
	var customerClock string
	if err := tx.Raw(`SELECT COALESCE(last_customer_message_at::text, '')
	                  FROM unofficial_whatsapp_conversations WHERE id = ?`,
		conversationID).Scan(&customerClock).Error; err != nil {
		t.Fatalf("last_customer_message_at: %v", err)
	}
	if customerClock == "" {
		t.Error("a thread ending on the lead's turn did not set last_customer_message_at")
	}

	// Every row is marked, so these are findable and deletable later without
	// guessing from the text.
	var marked int64
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages
		WHERE entry_id = ? AND entry_type = 'unofficial_whatsapp' AND deleted_at IS NULL
		  AND metadata->>'seed' = 'lead_import'`, conversationID).Scan(&marked).Error; err != nil {
		t.Fatalf("metadata count: %v", err)
	}
	if marked != 4 {
		t.Errorf("%d of 4 messages carry the seed marker", marked)
	}
}

// Re-running a scripted import must not write a second thread, and must not
// call the model again.
func TestSeedInboxScriptedSeedingIsIdempotentAgainstPostgres(t *testing.T) {
	tx := integrationTx(t)
	workspaceID, _ := anyConnectedInstance(t, tx)

	const number = "5511900000004"
	seeder := newIntegrationSeederWith(tx, scriptedFake{})
	req := uw.SeedRequest{
		WorkspaceID: workspaceID,
		Targets:     []uw.SeedTarget{{Number: number, Name: "Integracao Vozko"}},
		Script: &uw.SeedScript{
			Bodies:      []string{"Oi {{1}}, tudo bem?"},
			MaxMessages: 4,
		},
	}

	if _, err := seeder.Execute(context.Background(), req); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	out, err := seeder.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if out.AlreadyActive != 1 || out.Scripted != 0 {
		t.Fatalf("second run outcome = %+v, want 1 already active and nothing scripted", out)
	}

	conversationID := locateSeededConversation(t, tx, workspaceID, number)
	if got := countSeededMessages(t, tx, conversationID); got != 4 {
		t.Fatalf("two runs left %d messages, want 4", got)
	}
}

// A scripter that fails must leave a conversation that still renders, with
// today's placeholder in it. The degradation the whole feature promises,
// checked against the gates rather than against a fake.
func TestSeedInboxFallsBackToAVisiblePlaceholderAgainstPostgres(t *testing.T) {
	tx := integrationTx(t)
	workspaceID, _ := anyConnectedInstance(t, tx)

	const number = "5511900000005"
	seeder := newIntegrationSeederWith(tx, failingScripter{})

	out, err := seeder.Execute(context.Background(), uw.SeedRequest{
		WorkspaceID: workspaceID,
		Targets:     []uw.SeedTarget{{Number: number, Name: "Integracao Vozko"}},
		Script:      &uw.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Seeded != 1 || out.Scripted != 0 || out.ScriptFailed != 1 {
		t.Fatalf("outcome = %+v, want 1 seeded plain and 1 script failed", out)
	}

	conversationID := locateSeededConversation(t, tx, workspaceID, number)
	if !visibleInInbox(t, tx, workspaceID, conversationID) {
		t.Fatal("the fallback conversation does not pass the production inbox gates")
	}
	if got := countSeededMessages(t, tx, conversationID); got != 1 {
		t.Fatalf("the fallback wrote %d messages, want the 1 placeholder", got)
	}
	if _, unread := inboxPreview(t, tx, conversationID); unread != 0 {
		t.Errorf("the placeholder fallback has %d unread messages, want 0", unread)
	}
}

// The whole suite runs inside a transaction that is always rolled back, so the
// development database is untouched. This is the test that says so: it counts
// the rows a scripted seed would add, in a nested transaction it aborts itself,
// and asserts the counts come back to where they started.
func TestSeedInboxScriptedSeedingLeavesNoRowsBehind(t *testing.T) {
	tx := integrationTx(t)
	workspaceID, _ := anyConnectedInstance(t, tx)

	before := seededRowCounts(t, tx, workspaceID)

	inner := tx.Begin()
	if inner.Error != nil {
		t.Fatalf("savepoint: %v", inner.Error)
	}
	const number = "5511900000006"
	if _, err := newIntegrationSeederWith(inner, scriptedFake{}).
		Execute(context.Background(), uw.SeedRequest{
			WorkspaceID: workspaceID,
			Targets:     []uw.SeedTarget{{Number: number, Name: "Integracao Vozko"}},
			Script:      &uw.SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4},
		}); err != nil {
		inner.Rollback()
		t.Fatalf("Execute: %v", err)
	}
	during := seededRowCounts(t, inner, workspaceID)
	if during.messages <= before.messages || during.conversations <= before.conversations {
		inner.Rollback()
		t.Fatalf("seeding wrote nothing: before %+v, during %+v", before, during)
	}
	if err := inner.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}

	after := seededRowCounts(t, tx, workspaceID)
	if after != before {
		t.Fatalf("rows survived the rollback: before %+v, after %+v", before, after)
	}
}

type seededCounts struct {
	conversations int64
	contacts      int64
	messages      int64
}

func seededRowCounts(t *testing.T, tx *gorm.DB, workspaceID string) seededCounts {
	t.Helper()
	var counts seededCounts
	if err := tx.Raw(`SELECT COUNT(*) FROM unofficial_whatsapp_conversations
	                  WHERE workspace_id = ? AND deleted_at IS NULL`, workspaceID).
		Scan(&counts.conversations).Error; err != nil {
		t.Fatalf("conversation count: %v", err)
	}
	if err := tx.Raw(`SELECT COUNT(*) FROM unofficial_whatsapp_contacts
	                  WHERE workspace_id = ? AND deleted_at IS NULL`, workspaceID).
		Scan(&counts.contacts).Error; err != nil {
		t.Fatalf("contact count: %v", err)
	}
	if err := tx.Raw(`
		SELECT COUNT(*) FROM conversation_messages cm
		JOIN unofficial_whatsapp_conversations uwc ON uwc.id = cm.entry_id
		WHERE uwc.workspace_id = ? AND cm.entry_type = 'unofficial_whatsapp'
		  AND cm.deleted_at IS NULL`, workspaceID).
		Scan(&counts.messages).Error; err != nil {
		t.Fatalf("message count: %v", err)
	}
	return counts
}
