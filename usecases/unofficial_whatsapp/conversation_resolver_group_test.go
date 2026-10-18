package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

type leadLinkerThatMustNotRun struct{ t *testing.T }

func (l leadLinkerThatMustNotRun) EnsureLeadForPhone(context.Context, string, string, string) (string, error) {
	l.t.Fatal("a group conversation was bridged to a lead")
	return "", nil
}

func TestResolveGroupOpensTheGroupConversationWithoutALead(t *testing.T) {
	contacts, conversations := newFakeContactRepo(), newFakeConversationRepo()
	resolver := NewConversationResolver(contacts, conversations, leadLinkerThatMustNotRun{t})
	instance := &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1"}

	resolved, err := resolver.ResolveGroup(context.Background(), instance, "120363000000000001@g.us", "Ofertas VIP #1")
	if err != nil {
		t.Fatalf("ResolveGroup: %v", err)
	}
	if !resolved.Conversation.IsGroup || resolved.Conversation.ChatID != "120363000000000001@g.us" || resolved.Conversation.CampaignID != "" {
		t.Fatalf("conversation = %+v, want the group's own conversation", resolved.Conversation)
	}
	if !resolved.Contact.IsGroup {
		t.Fatalf("contact = %+v, want the group contact", resolved.Contact)
	}
}

func TestResolveGroupReusesTheConversationTheInboxAlreadyHas(t *testing.T) {
	existing := &uw.Conversation{ID: "conv-existing", InstanceID: "inst-1", ChatID: "120363000000000001@g.us", IsGroup: true}
	resolver := NewConversationResolver(newFakeContactRepo(), newFakeConversationRepo(existing), nil)

	resolved, err := resolver.ResolveGroup(context.Background(), &uw.Instance{ID: "inst-1", WorkspaceID: "ws-1"}, "120363000000000001@g.us", "")
	if err != nil || resolved.Conversation.ID != "conv-existing" || !resolved.AlreadyExisted {
		t.Fatalf("ResolveGroup = %+v, %v; want the existing conversation", resolved, err)
	}
}

func TestResolveGroupRefusesAContactJID(t *testing.T) {
	resolver := NewConversationResolver(newFakeContactRepo(), newFakeConversationRepo(), nil)
	_, err := resolver.ResolveGroup(context.Background(), &uw.Instance{ID: "inst-1"}, "5511981124410@s.whatsapp.net", "")
	if !errors.Is(err, uw.ErrNotAGroupJID) {
		t.Fatalf("ResolveGroup error = %v, want ErrNotAGroupJID", err)
	}
}
