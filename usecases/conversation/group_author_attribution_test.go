package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// Who wrote each message in a group thread.
//
// A message row stores no sender NAME on purpose — a frozen name goes stale the
// moment someone renames themselves — so on a live push the name rides along and
// on a reload the reader has to resolve it again. It used to resolve it from the
// conversation's subject, and in a group the subject is the GROUP: every bubble
// in the thread was labelled with the group's own name and its own picture, as
// if the group had been talking to itself.
//
// These pin the resolution and, just as importantly, pin that it stays free for
// the ordinary one-to-one conversation, which is almost all of them.

// authorLookup is a ContactIdentityLookup that records what was asked of it.
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

// The bug, stated as a test: three people talking in a group are three names,
// not three copies of the group's.
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
	// A group's handle is empty: the "number" slot does not exist for one.
	authors := svc.authorsFor(groupEntryType, "conv-1", "", messages)

	for _, msg := range messages {
		// What the subject-derived pass would have left behind.
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

// One query for the page, not one per bubble. A busy group is the only place
// this runs, so an N+1 here would be an N+1 exactly where the pages are longest.
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

// The ordinary one-to-one conversation costs nothing.
//
// There the author IS the subject, whom getSenderInfo already named, so there is
// no query to issue. This is the case that decides whether the fix is free for
// the overwhelming majority of conversations.
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

// Outbound messages are left alone. An operator's reply is attributed from the
// user record, and `From` there is the instance's own label — resolving it as a
// participant would relabel every reply with the number that sent it.
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

// A participant with no photo shows NO photo — never the group's.
//
// The empty picture has to overwrite, not fall through. Falling through is the
// version of this bug that survives a half-fix: the name becomes right and the
// face stays the group's.
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

// An author who resolves to no contact keeps whatever the subject pass gave
// them. Naming them "" would be worse than naming them imprecisely.
func TestUnresolvedAuthorKeepsTheFallbackName(t *testing.T) {
	svc := serviceWith(&authorLookup{byHandle: map[string]ContactDisplay{}})

	msg := inbound("+5511900000009")
	msg.SenderName, msg.SenderAvatar = "Equipe Vozko", "https://cdn.test/group.jpg"
	applyAuthor(msg, svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{msg}))

	if msg.SenderName != "Equipe Vozko" || msg.SenderAvatar != "https://cdn.test/group.jpg" {
		t.Errorf("got %q / %q, want the fallback untouched", msg.SenderName, msg.SenderAvatar)
	}
}

// A failed lookup degrades to the old behaviour rather than failing the read.
// This is a label on a bubble; it is never a reason to refuse a conversation.
func TestLookupFailureDoesNotBreakTheRead(t *testing.T) {
	svc := serviceWith(&authorLookup{err: errors.New("db down")})

	authors := svc.authorsFor(groupEntryType, "conv-1", "", []*conversation.Message{
		inbound("+5511900000001"),
	})
	if authors != nil {
		t.Errorf("authors = %v, want nil on failure", authors)
	}
}

// A channel with no identity lookup registered — the official WhatsApp entry,
// whose senders are leads — must not reach any of this.
func TestChannelWithoutAnIdentityLookupIsUntouched(t *testing.T) {
	svc := &HistoryProviderService{}

	if got := svc.authorsFor(shared.EntryTypeWhatsApp, "entry-1", "", []*conversation.Message{
		inbound("+5511900000001"),
	}); got != nil {
		t.Errorf("authors = %v, want nil", got)
	}
}

// applyAuthor must tolerate the shapes the callers actually hand it: a nil
// message from a sparse page, and the nil map every non-group read produces.
func TestApplyAuthorTolerance(t *testing.T) {
	applyAuthor(nil, map[string]ContactDisplay{"+55": {Name: "Ana"}})

	msg := inbound("+5511900000001")
	msg.SenderName = "Equipe Vozko"
	applyAuthor(msg, nil)
	if msg.SenderName != "Equipe Vozko" {
		t.Errorf("name = %q, want it untouched", msg.SenderName)
	}
}
