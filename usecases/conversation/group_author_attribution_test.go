package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type authorLookup struct {
	byHandle map[string]ContactDisplay
	err      error
	calls    [][]string
}

func (l *authorLookup) ContactsByIDs(context.Context, []string) (map[string]ContactDisplay, error) {
	return nil, nil
}

func (l *authorLookup) ContactForConversation(context.Context, string) (ContactDisplay, string, error) {
	return ContactDisplay{}, "", nil
}

func (l *authorLookup) AuthorsByHandle(_ context.Context, _ string, handles []string) (map[string]ContactDisplay, error) {
	l.calls = append(l.calls, handles)
	if l.err != nil {
		return nil, l.err
	}
	out := make(map[string]ContactDisplay, len(handles))
	for _, h := range handles {
		if display, ok := l.byHandle[h]; ok {
			out[h] = display
		}
	}
	return out, nil
}

const groupEntryType = shared.EntryTypeUnofficialWhatsApp

func serviceWith(lookup ContactIdentityLookup) *HistoryProviderService {
	return &HistoryProviderService{
		contactIdentities: map[shared.EntryType]ContactIdentityLookup{groupEntryType: lookup},
	}
}

func inbound(from string) *conversation.Message {
	return &conversation.Message{From: from, MessageType: conversation.MessageTypeUserMessage}
}

func TestGroupMessagesAreAttributedToTheirAuthors(t *testing.T) {
	lookup := &authorLookup{byHandle: map[string]ContactDisplay{
		"+5511900000001": {Name: "Ana", PictureURL: "https://cdn.test/ana.jpg"},
		"+5511900000002": {Name: "Bruno", PictureURL: "https://cdn.test/bruno.jpg"},
	}}
	svc := serviceWith(lookup)

	messages := []*conversation.Message{
		inbound("+5511900000001"),
		inbound("+5511900000002"),
		inbound("+5511900000001"),
	}
	authors := svc.authorsFor(groupEntryType, "conv-1", "", messages)

	for _, msg := range messages {
		msg.SenderName, msg.SenderAvatar = "Equipe Vozko", "https://cdn.test/group.jpg"
		applyAuthor(msg, authors)
	}

	want := []struct{ name, avatar string }{
		{"Ana", "https://cdn.test/ana.jpg"},
		{"Bruno", "https://cdn.test/bruno.jpg"},
		{"Ana", "https://cdn.test/ana.jpg"},
	}
	for i, w := range want {
		if messages[i].SenderName != w.name {
			t.Errorf("message %d: name = %q, want %q", i, messages[i].SenderName, w.name)
		}
		if messages[i].SenderAvatar != w.avatar {
			t.Errorf("message %d: avatar = %q, want %q", i, messages[i].SenderAvatar, w.avatar)
		}
	}
}

func TestAuthorLookupIsBatchedAndDeduplicated(t *testing.T) {
	lookup := &authorLookup{byHandle: map[string]ContactDisplay{}}
	svc := serviceWith(lookup)

	svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{
		inbound("+551190001"), inbound("+551190002"), inbound("+551190001"),
		inbound("+551190002"), inbound("+551190003"),
	})

	if len(lookup.calls) != 1 {
		t.Fatalf("%d lookups for one page, want 1", len(lookup.calls))
	}
	if got := len(lookup.calls[0]); got != 3 {
		t.Errorf("asked for %d handles, want the 3 distinct ones: %v", got, lookup.calls[0])
	}
}

func TestOneToOneConversationIssuesNoLookup(t *testing.T) {
	lookup := &authorLookup{}
	svc := serviceWith(lookup)

	authors := svc.authorsFor(groupEntryType, "conv-1", "+5511988887777", []*conversation.Message{
		inbound("+5511988887777"),
		inbound("+5511988887777"),
	})

	if len(lookup.calls) != 0 {
		t.Errorf("a one-to-one thread issued %d lookups: %v", len(lookup.calls), lookup.calls)
	}
	if authors != nil {
		t.Errorf("authors = %v, want nil", authors)
	}
}

func TestOutboundMessagesAreNotLookedUp(t *testing.T) {
	lookup := &authorLookup{}
	svc := serviceWith(lookup)

	svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{
		{From: "Comercial", MessageType: conversation.MessageTypeOperator},
		{From: "Comercial", MessageType: conversation.MessageTypeAIResponse},
	})

	if len(lookup.calls) != 0 {
		t.Errorf("outbound messages were looked up: %v", lookup.calls)
	}
}

func TestAuthorWithoutPictureDoesNotInheritTheGroups(t *testing.T) {
	lookup := &authorLookup{byHandle: map[string]ContactDisplay{
		"+5511900000001": {Name: "Ana"},
	}}
	svc := serviceWith(lookup)

	msg := inbound("+5511900000001")
	msg.SenderName, msg.SenderAvatar = "Equipe Vozko", "https://cdn.test/group.jpg"
	applyAuthor(msg, svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{msg}))

	if msg.SenderName != "Ana" {
		t.Errorf("name = %q, want Ana", msg.SenderName)
	}
	if msg.SenderAvatar != "" {
		t.Errorf("avatar = %q; the author inherited the group's picture", msg.SenderAvatar)
	}
}

func TestUnresolvedAuthorKeepsTheFallbackName(t *testing.T) {
	svc := serviceWith(&authorLookup{byHandle: map[string]ContactDisplay{}})

	msg := inbound("+5511900000009")
	msg.SenderName, msg.SenderAvatar = "Equipe Vozko", "https://cdn.test/group.jpg"
	applyAuthor(msg, svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{msg}))

	if msg.SenderName != "Equipe Vozko" || msg.SenderAvatar != "https://cdn.test/group.jpg" {
		t.Errorf("got %q / %q, want the fallback untouched", msg.SenderName, msg.SenderAvatar)
	}
}

func TestLookupFailureDoesNotBreakTheRead(t *testing.T) {
	svc := serviceWith(&authorLookup{err: errors.New("db down")})

	authors := svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{
		inbound("+5511900000001"),
	})
	if authors != nil {
		t.Errorf("authors = %v, want nil on failure", authors)
	}
}

func TestChannelWithoutAnIdentityLookupIsUntouched(t *testing.T) {
	svc := &HistoryProviderService{}

	if got := svc.authorsFor(shared.EntryTypeWhatsApp, "entry-1", "", []*conversation.Message{
		inbound("+5511900000001"),
	}); got != nil {
		t.Errorf("authors = %v, want nil", got)
	}
}

func TestApplyAuthorTolerance(t *testing.T) {
	applyAuthor(nil, map[string]ContactDisplay{"+55": {Name: "Ana"}})

	msg := inbound("+5511900000001")
	msg.SenderName = "Equipe Vozko"
	applyAuthor(msg, nil)
	if msg.SenderName != "Equipe Vozko" {
		t.Errorf("name = %q, want it untouched", msg.SenderName)
	}
}
