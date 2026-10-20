package conversation_repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTheCustomerSpokeAfterARunOnlyWhenTheyWroteLaterAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedTx(t, "spoke_after_test", &schema.ConversationMessage{})
	repo := &repository{db: db}
	entryID, otherEntryID := uuid.NewString(), uuid.NewString()
	runEnded := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	insert := func(entryID string, entryType shared.EntryType, senderKind string, at time.Time, deletedAt *time.Time) {
		t.Helper()
		if err := db.Exec(`INSERT INTO conversation_messages (id, entry_id, entry_type, message_type, from_participant, text, sender_kind, created_at, updated_at, deleted_at)
			VALUES (?, ?, ?, 'user_message', 'x', 'oi', ?, ?, ?, ?)`,
			uuid.NewString(), entryID, string(entryType), senderKind, at, at, deletedAt).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	removed := runEnded.Add(time.Hour)

	insert(entryID, shared.EntryTypeTelegram, "contact", runEnded.Add(-time.Second), nil)
	insert(entryID, shared.EntryTypeTelegram, "contact", runEnded, nil)
	insert(entryID, shared.EntryTypeTelegram, "ai", runEnded.Add(time.Minute), nil)
	insert(entryID, shared.EntryTypeTelegram, "workflow", runEnded.Add(time.Minute), nil)
	insert(entryID, shared.EntryTypeTelegram, "contact", runEnded.Add(time.Minute), &removed)
	insert(entryID, shared.EntryTypeInstagram, "contact", runEnded.Add(time.Minute), nil)
	insert(otherEntryID, shared.EntryTypeTelegram, "contact", runEnded.Add(time.Minute), nil)

	spoke, err := repo.CustomerSpokeAfter(entryID, shared.EntryTypeTelegram, runEnded)
	if err != nil || spoke {
		t.Fatalf("spoke = %v, err = %v; only earlier, same-instant, automated, deleted or other-conversation messages exist", spoke, err)
	}

	insert(entryID, shared.EntryTypeTelegram, "contact", runEnded.Add(time.Second), nil)
	spoke, err = repo.CustomerSpokeAfter(entryID, shared.EntryTypeTelegram, runEnded)
	if err != nil || !spoke {
		t.Fatalf("spoke = %v, err = %v; the customer wrote after the run ended", spoke, err)
	}

	if err := db.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
		t.Fatal(err)
	}
	rows, err := db.Raw("EXPLAIN "+customerSpokeAfterSQL, entryID, string(shared.EntryTypeTelegram), runEnded).Rows()
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	joined := strings.Join(plan, "\n")
	if strings.Contains(joined, "Seq Scan") || !strings.Contains(joined, "Index") || !strings.Contains(joined, "conversation_messages") {
		t.Fatalf("the existence check must be served by a conversation message index:\n%s", joined)
	}
}
