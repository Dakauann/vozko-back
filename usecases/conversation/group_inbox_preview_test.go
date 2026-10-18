package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type groupDirectory struct {
	contacts     map[string]ContactDisplay
	members      map[string]map[string]ContactDisplay
	err          error
	accountCalls []map[string][]string
}

func (d *groupDirectory) ContactsByIDs(_ context.Context, ids []string) (map[string]ContactDisplay, error) {
	out := map[string]ContactDisplay{}
	for _, id := range ids {
		if c, ok := d.contacts[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func (d *groupDirectory) ContactForConversation(context.Context, string) (ContactDisplay, string, error) {
	return ContactDisplay{}, "", nil
}

func (d *groupDirectory) AuthorsByHandle(context.Context, string, []string) (map[string]ContactDisplay, error) {
	return nil, nil
}

func (d *groupDirectory) AuthorsByAccount(_ context.Context, handlesByAccount map[string][]string) (map[string]map[string]ContactDisplay, error) {
	d.accountCalls = append(d.accountCalls, handlesByAccount)
	if d.err != nil {
		return nil, d.err
	}
	out := map[string]map[string]ContactDisplay{}
	for account, handles := range handlesByAccount {
		for _, handle := range handles {
			if member, ok := d.members[account][handle]; ok {
				if out[account] == nil {
					out[account] = map[string]ContactDisplay{}
				}
				out[account][handle] = member
			}
		}
	}
	return out, nil
}

func groupRow(entryID, contactID, account, from string, kind conversation.SenderKind, senderID string, messageType conversation.MessageType) conversation.EntryWithLastMessage {
	return conversation.EntryWithLastMessage{
		EntryID:               entryID,
		EntryType:             shared.EntryTypeUnofficialWhatsApp,
		LeadID:                contactID,
		BusinessPhoneID:       account,
		LastMessageText:       "Olá",
		LastMessageType:       messageType,
		LastMessageAt:         time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
		LastMessageFrom:       from,
		LastMessageSenderKind: string(kind),
		LastMessageSenderID:   senderID,
	}
}

func groupInboxService(directory *groupDirectory) *HistoryProviderService {
	svc := &HistoryProviderService{}
	svc.SetContactIdentityLookup(shared.EntryTypeUnofficialWhatsApp, directory)
	return svc
}

func TestGroupInboxPreviewNamesTheMemberWhoSentTheLastMessage(t *testing.T) {
	directory := &groupDirectory{
		contacts: map[string]ContactDisplay{
			"group-a": {ContactID: "group-a", Name: "Equipe Vendas", IsGroup: true},
			"group-b": {ContactID: "group-b", Name: "Familia", IsGroup: true},
		},
		members: map[string]map[string]ContactDisplay{
			"instance-1": {
				"+5511900000001": {Name: "Rita"},
				"+5511900000002": {Name: "Bruno"},
			},
		},
	}
	svc := groupInboxService(directory)

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		groupRow("conv-a", "group-a", "instance-1", "+5511900000001", conversation.SenderContact, "+5511900000001", conversation.MessageTypeUserMessage),
		groupRow("conv-b", "group-b", "instance-1", "+5511900000002", conversation.SenderContact, "+5511900000002", conversation.MessageTypeUserMessage),
	}, "")

	if entries[0].LastMessageAuthor != "Rita" || entries[1].LastMessageAuthor != "Bruno" {
		t.Fatalf("authors = %q, %q; want Rita, Bruno", entries[0].LastMessageAuthor, entries[1].LastMessageAuthor)
	}
	if entries[0].LastMessageFromUs || entries[1].LastMessageFromUs {
		t.Errorf("a member's message was marked as ours")
	}
	if len(directory.accountCalls) != 1 {
		t.Fatalf("%d author lookups for one page, want 1", len(directory.accountCalls))
	}
	if got := directory.accountCalls[0]["instance-1"]; len(got) != 2 {
		t.Errorf("handles asked for instance-1 = %v, want both distinct authors", got)
	}
}

func TestGroupInboxPreviewMarksOurOwnLastMessage(t *testing.T) {
	directory := &groupDirectory{contacts: map[string]ContactDisplay{
		"group-a": {ContactID: "group-a", Name: "Equipe Vendas", IsGroup: true},
		"group-b": {ContactID: "group-b", Name: "Familia", IsGroup: true},
	}}
	svc := groupInboxService(directory)

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		groupRow("conv-a", "group-a", "instance-1", "Comercial", conversation.SenderExternal, "", conversation.MessageTypeUserMessage),
		groupRow("conv-b", "group-b", "instance-1", "Comercial", conversation.SenderAI, "ai:agent-1", conversation.MessageTypeAIResponse),
	}, "")

	for i := range entries {
		if !entries[i].LastMessageFromUs {
			t.Errorf("entry %d: our own message not marked as ours", i)
		}
		if entries[i].LastMessageAuthor != "" {
			t.Errorf("entry %d: author = %q, want none for our own message", i, entries[i].LastMessageAuthor)
		}
	}
	if len(directory.accountCalls) != 0 {
		t.Errorf("our own messages were looked up: %v", directory.accountCalls)
	}
}

func TestOneToOneInboxPreviewCarriesNoAuthor(t *testing.T) {
	directory := &groupDirectory{contacts: map[string]ContactDisplay{
		"contact-1": {ContactID: "contact-1", Name: "Rita", Handle: "+5511900000001"},
	}}
	svc := groupInboxService(directory)

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		groupRow("conv-1", "contact-1", "instance-1", "+5511900000001", conversation.SenderContact, "+5511900000001", conversation.MessageTypeUserMessage),
		groupRow("conv-2", "contact-1", "instance-1", "Comercial", conversation.SenderExternal, "", conversation.MessageTypeUserMessage),
	}, "")

	for i := range entries {
		if entries[i].LastMessageAuthor != "" || entries[i].LastMessageFromUs {
			t.Errorf("entry %d: one-to-one preview carries an author: %+v", i, entries[i])
		}
	}
	if len(directory.accountCalls) != 0 {
		t.Errorf("one-to-one rows were looked up: %v", directory.accountCalls)
	}
}

func TestGroupInboxPreviewFallsBackToThePhoneWhenTheMemberIsUnknown(t *testing.T) {
	directory := &groupDirectory{
		contacts: map[string]ContactDisplay{
			"group-a": {ContactID: "group-a", Name: "Equipe Vendas", IsGroup: true},
			"group-b": {ContactID: "group-b", Name: "Familia", IsGroup: true},
		},
		members: map[string]map[string]ContactDisplay{
			"instance-1": {"123456789@lid": {Name: "123456789@lid", Ref: "123456789@lid"}},
		},
	}
	svc := groupInboxService(directory)

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		groupRow("conv-a", "group-a", "instance-1", "+5511900000009", conversation.SenderContact, "+5511900000009", conversation.MessageTypeUserMessage),
		groupRow("conv-b", "group-b", "instance-1", "123456789@lid", conversation.SenderContact, "123456789@lid", conversation.MessageTypeUserMessage),
	}, "")

	if entries[0].LastMessageAuthor != "+5511900000009" {
		t.Errorf("author = %q, want the phone number", entries[0].LastMessageAuthor)
	}
	if entries[1].LastMessageAuthor != "" {
		t.Errorf("author = %q, want no raw JID in the preview", entries[1].LastMessageAuthor)
	}
}

func TestGroupInboxPreviewSurvivesALookupFailure(t *testing.T) {
	directory := &groupDirectory{
		contacts: map[string]ContactDisplay{"group-a": {ContactID: "group-a", Name: "Equipe Vendas", IsGroup: true}},
		err:      errors.New("db down"),
	}
	svc := groupInboxService(directory)

	entries := svc.buildInboxEntries([]conversation.EntryWithLastMessage{
		groupRow("conv-a", "group-a", "instance-1", "+5511900000001", conversation.SenderContact, "+5511900000001", conversation.MessageTypeUserMessage),
	}, "")

	if len(entries) != 1 || entries[0].LastMessagePreview != "Olá" {
		t.Fatalf("the page broke on a lookup failure: %+v", entries)
	}
	if entries[0].LastMessageAuthor != "+5511900000001" {
		t.Errorf("author = %q, want the phone fallback", entries[0].LastMessageAuthor)
	}
}
