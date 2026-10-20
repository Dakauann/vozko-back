package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

type threadCall struct {
	entryID   string
	entryType shared.EntryType
	before    *time.Time
	limit     int
}

type fakeThread struct {
	messages []*conversation.Message
	hasMore  bool
	calls    []threadCall
}

func (f *fakeThread) GetHistory(entryID string, entryType shared.EntryType, limit int) ([]*conversation.Message, bool, int64, error) {
	f.calls = append(f.calls, threadCall{entryID: entryID, entryType: entryType, limit: limit})
	return f.messages, f.hasMore, int64(len(f.messages)), nil
}

func (f *fakeThread) GetHistoryBefore(entryID string, entryType shared.EntryType, before time.Time, limit int) ([]*conversation.Message, bool, error) {
	f.calls = append(f.calls, threadCall{entryID: entryID, entryType: entryType, before: &before, limit: limit})
	return f.messages, f.hasMore, nil
}

type fakeContacts struct {
	contacts []*uw.Contact
	asked    map[string][]string
}

func (f *fakeContacts) FindByHandles(_ context.Context, instanceID string, handles []string) ([]*uw.Contact, error) {
	if f.asked == nil {
		f.asked = map[string][]string{}
	}
	f.asked[instanceID] = append(f.asked[instanceID], handles...)
	var out []*uw.Contact
	for _, c := range f.contacts {
		if c.InstanceID == instanceID {
			out = append(out, c)
		}
	}
	return out, nil
}

func feedWorld(t *testing.T) (*world, *fakeThread, *fakeContacts) {
	w := newWorld()
	thread := &fakeThread{}
	contacts := &fakeContacts{}
	w.svc.d.Thread = thread
	w.svc.d.Contacts = contacts
	return w, thread, contacts
}

func at(minutes int) time.Time {
	return testNow.Add(-time.Duration(minutes) * time.Minute)
}

func inbound(id, from, text string, when time.Time) *conversation.Message {
	return &conversation.Message{
		ID: id, EntryID: "conv-1", EntryType: shared.EntryTypeUnofficialWhatsApp, MessageType: conversation.MessageTypeUserMessage,
		Direction: conversation.MessageDirectionInbound, From: from, Text: text, SenderName: "Grupo de vendas", CreatedAt: when,
		SentBy: conversation.SentByContact(from),
	}
}

func outbound(id, operatorID, text string, when time.Time) *conversation.Message {
	return &conversation.Message{
		ID: id, EntryID: "conv-1", EntryType: shared.EntryTypeUnofficialWhatsApp, MessageType: conversation.MessageTypeOperator,
		Direction: conversation.MessageDirectionOutbound, From: "5511970001201", Text: text, SenderName: operatorID,
		SenderAvatar: "https://img/operator.jpg", CreatedAt: when, SentBy: conversation.SentByPerson(operatorID),
	}
}

func TestGroupMessagesReadsTheLatestPageOfTheGroupConversation(t *testing.T) {
	w, thread, contacts := feedWorld(t)
	g := w.groupWithSeats(t, "120363@g.us", "Vendas", "n1")
	w.messages.conversations["n1|120363@g.us"] = "conv-1"
	w.members.byGroup[g.ID] = map[string]uwg.Member{
		"5511970001234": {PersonKey: "5511970001234", DisplayName: "Ana do grupo"},
	}
	contacts.contacts = []*uw.Contact{{InstanceID: "n1", PhoneNumber: "5511970005555", ContactName: "Bruno contato", PictureURL: "https://img/bruno.jpg"}}
	mediaID := "media-1"
	photo := inbound("m2", "+5511970005555", "", at(20))
	photo.MediaID, photo.MediaType = &mediaID, conversation.MediaTypeImage
	photo.Media = &conversation.AttachedMedia{URL: "https://cdn/photo.jpg", Layout: conversation.MediaLayout{Width: 800, Height: 600, Thumbhash: "abc"}}
	thread.messages = []*conversation.Message{
		inbound("m1", "+5511970001234", "Bom dia", at(30)),
		photo,
		outbound("m3", "7f9c2c1e-0000-4000-8000-000000000001", "Olá a todos", at(10)),
	}
	thread.hasMore = true

	page, err := w.svc.GroupMessages(context.Background(), everyone(), g.ID, GroupMessagesInput{})
	if err != nil {
		t.Fatal(err)
	}

	if len(thread.calls) != 1 || thread.calls[0].entryID != "conv-1" || thread.calls[0].entryType != shared.EntryTypeUnofficialWhatsApp || thread.calls[0].before != nil {
		t.Fatalf("thread calls = %+v, want one latest page of conv-1", thread.calls)
	}
	if thread.calls[0].limit != conversation.DefaultHistoryPageSize {
		t.Fatalf("limit = %d, want the default page size", thread.calls[0].limit)
	}
	if page.ConversationID != "conv-1" || !page.HasMore || page.NextBefore == nil || !page.NextBefore.Equal(at(30)) {
		t.Fatalf("page = %+v, want conv-1 with more before the oldest message", page)
	}
	if len(page.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(page.Messages))
	}
	first, second, third := page.Messages[0], page.Messages[1], page.Messages[2]
	if first.ID != "m1" || first.SenderName != "Ana do grupo" || first.OurTeam || first.Direction != conversation.MessageDirectionInbound {
		t.Errorf("first = %+v, want Ana from the members", first)
	}
	if second.SenderName != "Bruno contato" || second.SenderAvatar != "https://img/bruno.jpg" {
		t.Errorf("second = %+v, want Bruno from the contacts", second)
	}
	if second.Media == nil || second.Media.URL != "https://cdn/photo.jpg" || second.Media.Layout.Thumbhash != "abc" || second.MediaType != conversation.MediaTypeImage {
		t.Errorf("second media = %+v, want the attached media with its layout", second.Media)
	}
	if third.SenderName != uwg.OurTeamAuthor || !third.OurTeam || third.SenderAvatar != "" || third.From != "" {
		t.Errorf("third = %+v, want our team without the operator", third)
	}
	if got := contacts.asked["n1"]; len(got) != 2 {
		t.Errorf("contacts asked = %v, want the two inbound handles once each", got)
	}
}

func TestGroupMessagesPagesOlderMessagesBeforeTheCursor(t *testing.T) {
	w, thread, _ := feedWorld(t)
	g := w.groupWithSeats(t, "120363@g.us", "Vendas", "n1")
	w.messages.conversations["n1|120363@g.us"] = "conv-1"
	thread.messages = []*conversation.Message{inbound("m0", "+5511970001234", "antiga", at(300))}
	before := at(30)

	page, err := w.svc.GroupMessages(context.Background(), everyone(), g.ID, GroupMessagesInput{Before: &before, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.calls) != 1 || thread.calls[0].before == nil || !thread.calls[0].before.Equal(before) {
		t.Fatalf("thread calls = %+v, want a page before the cursor", thread.calls)
	}
	if thread.calls[0].limit != conversation.MaxHistoryPageSize {
		t.Fatalf("limit = %d, want the max page size", thread.calls[0].limit)
	}
	if page.HasMore || page.NextBefore != nil {
		t.Fatalf("page = %+v, want the start of the history", page)
	}
}

func TestGroupMessagesOutOfScopeGroupIsNotFound(t *testing.T) {
	w, thread, _ := feedWorld(t)
	g := w.groupWithSeats(t, "120363@g.us", "Suporte", "n2")
	w.messages.conversations["n2|120363@g.us"] = "conv-2"

	_, err := w.svc.GroupMessages(context.Background(), salesOnly(), g.ID, GroupMessagesInput{})
	if !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("err = %v, want ErrGroupNotFound", err)
	}
	if len(thread.calls) != 0 {
		t.Fatalf("thread read %d times for an out of scope group", len(thread.calls))
	}
}

func TestGroupMessagesReadsTheConversationOfAVisibleNumber(t *testing.T) {
	w, thread, _ := feedWorld(t)
	g := w.groupWithSeats(t, "120363@g.us", "Vendas", "n2", "n1")
	w.messages.conversations["n1|120363@g.us"] = "conv-sales"
	w.messages.conversations["n2|120363@g.us"] = "conv-support"

	page, err := w.svc.GroupMessages(context.Background(), salesOnly(), g.ID, GroupMessagesInput{})
	if err != nil {
		t.Fatal(err)
	}
	if page.ConversationID != "conv-sales" || len(thread.calls) != 1 || thread.calls[0].entryID != "conv-sales" {
		t.Fatalf("page = %+v calls = %+v, want the conversation of the number in scope", page, thread.calls)
	}
}

func TestGroupMessagesWithoutConversationIsAnEmptyHistory(t *testing.T) {
	w, thread, _ := feedWorld(t)
	g := w.groupWithSeats(t, "120363@g.us", "Vendas", "n1")

	page, err := w.svc.GroupMessages(context.Background(), everyone(), g.ID, GroupMessagesInput{})
	if err != nil {
		t.Fatal(err)
	}
	if page.ConversationID != "" || page.HasMore || page.Messages == nil || len(page.Messages) != 0 {
		t.Fatalf("page = %+v, want an empty history", page)
	}
	if len(thread.calls) != 0 {
		t.Fatalf("thread read without a conversation")
	}
}

func TestGroupMessagesRefusesWithoutAThreadReader(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363@g.us", "Vendas", "n1")
	w.messages.conversations["n1|120363@g.us"] = "conv-1"

	if _, err := w.svc.GroupMessages(context.Background(), everyone(), g.ID, GroupMessagesInput{}); !errors.Is(err, errFeedUnavailable) {
		t.Fatalf("err = %v, want errFeedUnavailable", err)
	}
}
