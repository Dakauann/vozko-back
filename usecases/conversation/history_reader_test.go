package conversation_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type accessStub struct {
	conversation.ConversationAuthorizer
	allow bool
	asked []string
}

func (a *accessStub) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	a.asked = append(a.asked, userID+"|"+workspaceID+"|"+entryID+"|"+entryType)
	return a.allow
}

type historyStub struct {
	latestLimit int
	beforeLimit int
	before      time.Time
}

func (h *historyStub) GetHistory(entryID string, entryType shared.EntryType, limit int) ([]*conversation.Message, bool, int64, error) {
	h.latestLimit = limit
	return []*conversation.Message{{ID: "m1"}}, true, 42, nil
}

func (h *historyStub) GetHistoryBefore(entryID string, entryType shared.EntryType, before time.Time, limit int) ([]*conversation.Message, bool, error) {
	h.beforeLimit = limit
	h.before = before
	return []*conversation.Message{{ID: "m0"}}, false, nil
}

func historyQuery() conversation.HistoryQuery {
	return conversation.HistoryQuery{
		Viewer:    conversation.Viewer{UserID: "u1", WorkspaceID: "ws1"},
		EntryID:   "e1",
		EntryType: shared.EntryTypeWhatsApp,
	}
}

func TestReadHistory_DeniesWhenTheViewerCannotSeeTheConversation(t *testing.T) {
	access := &accessStub{allow: false}
	history := &historyStub{}
	_, err := NewHistoryReader(access, history).ReadHistory(historyQuery())
	if !errors.Is(err, conversation.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
	if history.latestLimit != 0 {
		t.Fatal("history was read before access was granted")
	}
	if len(access.asked) != 1 || access.asked[0] != "u1|ws1|e1|whatsapp" {
		t.Fatalf("access asked with %v", access.asked)
	}
}

func TestReadHistory_FailsClosedWithoutAnAuthorizer(t *testing.T) {
	_, err := NewHistoryReader(nil, &historyStub{}).ReadHistory(historyQuery())
	if !errors.Is(err, conversation.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestReadHistory_RejectsAnUnviewableEntryType(t *testing.T) {
	q := historyQuery()
	q.EntryType = "nope"
	_, err := NewHistoryReader(&accessStub{allow: true}, &historyStub{}).ReadHistory(q)
	if !errors.Is(err, conversation.ErrEntryTypeInvalid) {
		t.Fatalf("err = %v, want ErrEntryTypeInvalid", err)
	}
}

func TestReadHistory_ReadsTheLatestPageWithAClampedSize(t *testing.T) {
	history := &historyStub{}
	q := historyQuery()
	q.Limit = 10_000
	page, err := NewHistoryReader(&accessStub{allow: true}, history).ReadHistory(q)
	if err != nil {
		t.Fatal(err)
	}
	if history.latestLimit != conversation.MaxHistoryPageSize {
		t.Fatalf("limit = %d, want %d", history.latestLimit, conversation.MaxHistoryPageSize)
	}
	if !page.HasMore || page.Total != 42 || len(page.Messages) != 1 {
		t.Fatalf("page = %+v", page)
	}
}

func TestReadHistory_ReadsBeforeACursor(t *testing.T) {
	history := &historyStub{}
	cursor := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	q := historyQuery()
	q.Before = &cursor
	page, err := NewHistoryReader(&accessStub{allow: true}, history).ReadHistory(q)
	if err != nil {
		t.Fatal(err)
	}
	if !history.before.Equal(cursor) || history.beforeLimit != conversation.DefaultHistoryPageSize {
		t.Fatalf("before = %v limit = %d", history.before, history.beforeLimit)
	}
	if page.Messages[0].ID != "m0" {
		t.Fatalf("page = %+v", page)
	}
}
