package container

import (
	"context"
	"testing"

	"vozko/domain/conversation"
	emdomain "vozko/domain/emailinbox"
	"vozko/domain/shared"
	"vozko/infra/emailwiring"
)

type emailIdentityContacts struct {
	emdomain.ContactRepository
	byID map[string]*emdomain.Contact
}

func (f emailIdentityContacts) FindByID(_ context.Context, id string) (*emdomain.Contact, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, emdomain.ErrContactNotFound
}

func (f emailIdentityContacts) FindByIDs(_ context.Context, ids []string) ([]*emdomain.Contact, error) {
	var out []*emdomain.Contact
	for _, id := range ids {
		if c, ok := f.byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

type emailIdentityConversations struct {
	emdomain.ConversationRepository
	byID map[string]*emdomain.Conversation
}

func (f emailIdentityConversations) FindByID(_ context.Context, id string) (*emdomain.Conversation, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, emdomain.ErrConversationNotFound
}

func TestEmailContactIdentityHydratesListsAndEntryUpdates(t *testing.T) {
	lead := "lead-1"
	contacts := emailIdentityContacts{byID: map[string]*emdomain.Contact{
		"c-1": {ID: "c-1", Address: "ana@cliente.com", DisplayName: "Ana Lima", LeadID: &lead},
		"c-2": {ID: "c-2", Address: "bia@cliente.com"},
	}}
	conversations := emailIdentityConversations{byID: map[string]*emdomain.Conversation{
		"conv-1": {ID: "conv-1", WorkspaceID: "ws-1", ContactID: "c-1"},
	}}
	lookup := emailContactIdentity(contacts, conversations)

	page, err := lookup.ContactsByIDs(context.Background(), []string{"c-1", "c-2", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page["c-1"].Name != "Ana Lima" || page["c-1"].LeadID != lead || page["c-2"].Name != "bia@cliente.com" || page["c-2"].Handle != "bia@cliente.com" {
		t.Fatalf("page = %+v", page)
	}

	display, workspace, err := lookup.ContactForConversation(context.Background(), "conv-1")
	if err != nil || workspace != "ws-1" || display.Name != "Ana Lima" || display.Ref != "ana@cliente.com" {
		t.Fatalf("entry update hydration = %+v %q %v", display, workspace, err)
	}
}

type emailAnalysisInboxes struct {
	emdomain.InboxRepository
	byID map[string]*emdomain.Inbox
}

func (f emailAnalysisInboxes) FindByIDUnscoped(_ context.Context, id string) (*emdomain.Inbox, error) {
	if i, ok := f.byID[id]; ok {
		return i, nil
	}
	return nil, emdomain.ErrInboxNotFound
}

func TestEmailAnalysisCarriesTheInboxSwitchesContainerAndLead(t *testing.T) {
	lead, agent := "lead-1", "agent-1"
	bundle := &emailInboxBundle{Inbox: &emailwiring.Inbox{
		Inboxes: emailAnalysisInboxes{byID: map[string]*emdomain.Inbox{
			"sender-1": {ID: "sender-1", WorkspaceID: "ws-1", Name: "Vendas", AgentID: &agent, EnableAnalysis: true, EnableAutoStaging: true, EnableAutoMemory: true},
		}},
		Contacts: emailIdentityContacts{byID: map[string]*emdomain.Contact{
			"c-1": {ID: "c-1", Address: "ana@cliente.com", DisplayName: "Ana Lima", LeadID: &lead},
		}},
		Conversations: emailIdentityConversations{byID: map[string]*emdomain.Conversation{
			"conv-1": {ID: "conv-1", WorkspaceID: "ws-1", ContactID: "c-1", SenderID: "sender-1"},
		}},
	}}

	subject, err := emailInboxAnalysisChannel(bundle).resolver(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if subject.EntryType != shared.EntryTypeEmail || subject.WorkspaceID != "ws-1" || subject.ContainerID != "sender-1" || subject.ContainerKind != conversation.ContainerKindAccount {
		t.Fatalf("placement lost: %+v", subject)
	}
	if !subject.EnableAnalysis || !subject.EnableAutoStaging || !subject.EnableAutoMemory || subject.AgentID != agent {
		t.Fatalf("inbox switches lost: %+v", subject)
	}
	if subject.LeadID != lead || subject.ContactLabel != "Ana Lima" || !subject.WantsWork() {
		t.Fatalf("memory subject lost: %+v", subject)
	}
}
