package conversation_usecase

import (
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type cursorMessageRepo struct {
	conversation.MessageRepository
	oldestFirst []*conversation.Message
	asked       conversation.ListMessagesInput
}

func (r *cursorMessageRepo) ListByEntryPaginated(in conversation.ListMessagesInput) ([]*conversation.Message, error) {
	r.asked = in
	var nearest []*conversation.Message
	for _, m := range r.oldestFirst {
		if in.After != nil && !m.CreatedAt.After(*in.After) {
			continue
		}
		nearest = append(nearest, m)
		if len(nearest) == in.Limit {
			break
		}
	}
	newestFirst := make([]*conversation.Message, 0, len(nearest))
	for i := len(nearest) - 1; i >= 0; i-- {
		newestFirst = append(newestFirst, nearest[i])
	}
	return newestFirst, nil
}

func timeline(start time.Time, n int) []*conversation.Message {
	out := make([]*conversation.Message, n)
	for i := range out {
		out[i] = &conversation.Message{ID: string(rune('a' + i)), EntryID: "e1", EntryType: shared.EntryTypeWhatsApp, Text: "oi", SenderName: "Ana", CreatedAt: start.Add(time.Duration(i) * time.Minute)}
	}
	return out
}

func TestHistoryAfterReturnsTheNextMessagesOldestFirstLikeEveryOtherPage(t *testing.T) {
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	repo := &cursorMessageRepo{oldestFirst: timeline(start, 6)}
	s := &HistoryProviderService{messageRepo: repo}

	messages, hasAfter, err := s.GetHistoryAfter("e1", shared.EntryTypeWhatsApp, start.Add(time.Minute), 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(messages); got != "cde" {
		t.Fatalf("page = %q, want the three messages right after the cursor, oldest first", got)
	}
	if !hasAfter {
		t.Fatal("a sixth message exists, so has_after must be true")
	}
	if repo.asked.Limit != 4 || repo.asked.Before != nil {
		t.Fatalf("asked = %+v, want one extra row to detect a next page", repo.asked)
	}

	last, hasAfter, err := s.GetHistoryAfter("e1", shared.EntryTypeWhatsApp, start.Add(3*time.Minute), 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(last); got != "ef" || hasAfter {
		t.Fatalf("page = %q has_after = %v, want the tail and no next page", got, hasAfter)
	}
}

func idsOf(messages []*conversation.Message) string {
	out := ""
	for _, m := range messages {
		out += m.ID
	}
	return out
}
