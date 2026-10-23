package container

import (
	"context"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

type carrierIdentityContacts struct {
	carriermessaging.ContactRepository
	byID map[string]*carriermessaging.Contact
}

func (f carrierIdentityContacts) FindByID(_ context.Context, id string) (*carriermessaging.Contact, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, carriermessaging.ErrContactNotFound
}

func (f carrierIdentityContacts) FindByIDs(_ context.Context, ids []string) ([]*carriermessaging.Contact, error) {
	var out []*carriermessaging.Contact
	for _, id := range ids {
		if c, ok := f.byID[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

type carrierIdentityConversations struct {
	carriermessaging.ConversationRepository
	byID map[string]*carriermessaging.Conversation
}

func (f carrierIdentityConversations) FindByID(_ context.Context, id string) (*carriermessaging.Conversation, error) {
	if c, ok := f.byID[id]; ok {
		return c, nil
	}
	return nil, carriermessaging.ErrConversationNotFound
}

type carrierAnalysisSenders struct {
	carriermessaging.SenderRepository
	sender *rcs.Sender
}

func (f carrierAnalysisSenders) FindByIDUnscoped(context.Context, string) (*rcs.Sender, error) {
	return f.sender, nil
}

type carrierAnalysisRoutes struct {
	carriermessaging.RouteRepository
	route *sms.Route
}

func (f carrierAnalysisRoutes) FindByIDUnscoped(context.Context, string) (*sms.Route, error) {
	return f.route, nil
}

func carrierIdentityFixture() (carrierIdentityContacts, carrierIdentityConversations, string) {
	lead := "lead-1"
	contacts := carrierIdentityContacts{byID: map[string]*carriermessaging.Contact{
		"c-1": {ID: "c-1", Phone: "5511999990000", DisplayName: "Ana Lima", LeadID: &lead},
		"c-2": {ID: "c-2", Phone: "5511999990001"},
	}}
	conversations := carrierIdentityConversations{byID: map[string]*carriermessaging.Conversation{
		"conv-1": {ID: "conv-1", WorkspaceID: "ws-1", SenderID: "sender-1", ContactID: "c-1"},
	}}
	return contacts, conversations, lead
}

func TestCarrierContactIdentityHydratesListsAndEntryUpdates(t *testing.T) {
	contacts, conversations, lead := carrierIdentityFixture()
	lookup := carrierContactIdentity(contacts, conversations)

	page, err := lookup.ContactsByIDs(context.Background(), []string{"c-1", "c-2", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page["c-1"].Name != "Ana Lima" || page["c-1"].LeadID != lead || page["c-2"].Name != "5511999990001" || page["c-2"].Ref != "5511999990001" {
		t.Fatalf("page = %+v", page)
	}
	display, workspace, err := lookup.ContactForConversation(context.Background(), "conv-1")
	if err != nil || workspace != "ws-1" || display.Name != "Ana Lima" || display.Ref != "5511999990000" {
		t.Fatalf("entry update hydration = %+v %q %v", display, workspace, err)
	}
}

func TestCarrierAnalysisResolversNameTheSenderAndCarryItsAutomation(t *testing.T) {
	contacts, conversations, lead := carrierIdentityFixture()
	agent := "agent-1"
	automation := conversation.ChannelAutomation{AgentID: &agent, EnableAnalysis: true}
	cases := []struct {
		entry    shared.EntryType
		resolver func() *carrierAnalysisSources
		label    string
	}{
		{shared.EntryTypeRCS, func() *carrierAnalysisSources {
			return &carrierAnalysisSources{Conversations: conversations, Contacts: contacts,
				Senders: carrierAnalysisSenders{sender: &rcs.Sender{ID: "sender-1", BrandName: "Loja", Automation: automation}}}
		}, "Loja"},
		{shared.EntryTypeSMS, func() *carrierAnalysisSources {
			return &carrierAnalysisSources{Conversations: conversations, Contacts: contacts,
				Routes: carrierAnalysisRoutes{route: &sms.Route{ID: "sender-1", BrandPrefix: "LOJA", Automation: automation}}}
		}, "LOJA"},
	}
	for _, tc := range cases {
		t.Run(string(tc.entry), func(t *testing.T) {
			subject, err := carrierAnalysisResolver(tc.entry, tc.resolver())(context.Background(), "conv-1")
			if err != nil || subject == nil {
				t.Fatalf("subject %+v %v", subject, err)
			}
			if subject.EntryType != tc.entry || subject.WorkspaceID != "ws-1" || subject.LeadID != lead ||
				subject.ContainerID != "sender-1" || subject.ContainerName != tc.label || subject.AgentID != agent || !subject.EnableAnalysis {
				t.Fatalf("subject = %+v", subject)
			}
		})
	}
}
