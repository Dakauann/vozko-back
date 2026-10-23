package conversation_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func messageStateRepo(t *testing.T) conversation.MessageRepository {
	t.Helper()
	return NewRepository(repotest.IsolatedDB(t, "msg_state", &schema.ConversationMessage{}))
}

func storedMessage(t *testing.T, repo conversation.MessageRepository, entryID string, at time.Time, text string) *conversation.Message {
	t.Helper()
	m := &conversation.Message{
		ID: uuid.NewString(), EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Channel: conversation.MessageChannelWhatsApp,
		MessageType: conversation.MessageTypeUserMessage, SentBy: conversation.SentByContact("5511"), From: "5511", Text: text,
		CreatedAt: at, UpdatedAt: at,
	}
	if err := repo.Create(m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPagingAfterACursorReturnsTheMessagesRightAfterItAgainstPostgres(t *testing.T) {
	repo := messageStateRepo(t)
	entryID := uuid.NewString()
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	var texts []string
	for i := 0; i < 6; i++ {
		texts = append(texts, storedMessage(t, repo, entryID, start.Add(time.Duration(i)*time.Minute), string(rune('a'+i))).Text)
	}

	after := start.Add(time.Minute)
	page, err := repo.ListByEntryPaginated(conversation.ListMessagesInput{EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Limit: 3, After: &after})
	if err != nil {
		t.Fatal(err)
	}
	if got := textsOf(page); got != "edc" {
		t.Fatalf("after page = %q, want the three messages right after the cursor, newest first like every page", got)
	}

	before := start.Add(4 * time.Minute)
	older, err := repo.ListByEntryPaginated(conversation.ListMessagesInput{EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Limit: 2, Before: &before})
	if err != nil {
		t.Fatal(err)
	}
	if got := textsOf(older); got != "dc" {
		t.Fatalf("before page = %q, want the two messages right before the cursor", got)
	}
}

func textsOf(messages []*conversation.Message) string {
	out := ""
	for _, m := range messages {
		out += m.Text
	}
	return out
}

func TestMessageStateRoundTripsAgainstPostgres(t *testing.T) {
	repo := messageStateRepo(t)
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	m := storedMessage(t, repo, uuid.NewString(), at, conversation.AudioPlaceholder)

	if err := m.React(conversation.Reaction{Emoji: "👍", Direction: conversation.ReactionInbound, ActorID: "5511", At: at}); err != nil {
		t.Fatal(err)
	}
	m.SetTranscript("quero o plano")
	m.ApplyEdit("", at)
	m.Retract(at.Add(time.Minute))
	if err := repo.Update(m.ID, m); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Transcript != "quero o plano" || got.EditedAt == nil || !got.EditedAt.Equal(at) || got.RetractedAt == nil || !got.RetractedAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("state = transcript %q edited %v retracted %v", got.Transcript, got.EditedAt, got.RetractedAt)
	}
	if reactions := got.Reactions(); len(reactions) != 1 || reactions[0].Emoji != "👍" {
		t.Fatalf("reactions = %+v", reactions)
	}
}

func TestMessageSearchMatchesTranscriptsAndSkipsRetractedMessagesAgainstPostgres(t *testing.T) {
	repo := messageStateRepo(t)
	entryID := uuid.NewString()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	spoken := storedMessage(t, repo, entryID, at, conversation.AudioPlaceholder)
	spoken.SetTranscript("Quero o PLANO anual")
	if err := repo.Update(spoken.ID, spoken); err != nil {
		t.Fatal(err)
	}
	storedMessage(t, repo, entryID, at.Add(time.Minute), "o plano mensal")
	deleted := storedMessage(t, repo, entryID, at.Add(2*time.Minute), "plano secreto")
	deleted.Retract(at.Add(3 * time.Minute))
	if err := repo.Update(deleted.ID, deleted); err != nil {
		t.Fatal(err)
	}

	found, total, err := repo.SearchMessagesByEntry(conversation.SearchMessagesByEntryInput{EntryID: entryID, EntryType: shared.EntryTypeWhatsApp, Query: "plano"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(found) != 2 {
		t.Fatalf("total = %d found = %d, want the transcript and the text, never the retracted one", total, len(found))
	}
	for _, m := range found {
		if m.ID == deleted.ID {
			t.Fatal("a retracted message must not be found")
		}
	}
}

func TestTheLastMessageKnowsItWasRetractedAgainstPostgres(t *testing.T) {
	repo := messageStateRepo(t)
	entryID := uuid.NewString()
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	last := storedMessage(t, repo, entryID, at, "segredo")
	last.Retract(at.Add(time.Minute))
	if err := repo.Update(last.ID, last); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetEntryLastMessage(entryID, shared.EntryTypeWhatsApp)
	if err != nil || got == nil {
		t.Fatalf("last = %+v err = %v", got, err)
	}
	if !got.LastMessageRetracted {
		t.Fatal("the inbox row must know its last message was retracted")
	}
}

func TestUpdateMovesTheDeliveryStatusOnlyForwardAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "msg_state", &schema.ConversationMessage{})
	repo := NewRepository(db)
	m := storedMessage(t, repo, uuid.NewString(), time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), "pedido enviado")
	legacy := storedMessage(t, repo, uuid.NewString(), time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), "linha antiga")
	if err := db.Exec("UPDATE conversation_messages SET delivery_status = NULL WHERE id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	legacy.DeliveryStatus = conversation.DeliveryStatusDelivered
	if err := repo.Update(legacy.ID, legacy); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetByID(legacy.ID); err != nil || got.DeliveryStatus != conversation.DeliveryStatusDelivered {
		t.Fatalf("a row without a stored status = %+v %v, want delivered", got, err)
	}
	steps := []struct {
		write conversation.DeliveryStatus
		want  conversation.DeliveryStatus
	}{
		{conversation.DeliveryStatusDelivered, conversation.DeliveryStatusDelivered},
		{conversation.DeliveryStatusSent, conversation.DeliveryStatusDelivered},
		{conversation.DeliveryStatusRead, conversation.DeliveryStatusRead},
		{conversation.DeliveryStatusDelivered, conversation.DeliveryStatusRead},
		{conversation.DeliveryStatusFailed, conversation.DeliveryStatusFailed},
		{conversation.DeliveryStatusDelivered, conversation.DeliveryStatusFailed},
	}
	for i, step := range steps {
		stale := *m
		stale.DeliveryStatus = step.write
		if err := repo.Update(m.ID, &stale); err != nil {
			t.Fatal(err)
		}
		got, err := repo.GetByID(m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DeliveryStatus != step.want {
			t.Fatalf("step %d wrote %q and stored %q, want %q", i, step.write, got.DeliveryStatus, step.want)
		}
	}
}
