package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func startFixture(t *testing.T, mutate func(*uw.Instance)) (
	*StartConversationUseCase, *fakeMessaging, *fakeContactRepo, *fakeConversationRepo,
) {
	t.Helper()

	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-a",
		Status: uw.StatusConnected, InstanceToken: "tok",
		PhoneNumber: "5511777777777",
	}
	if mutate != nil {
		mutate(instance)
	}

	messaging := &fakeMessaging{}
	contacts := newFakeContactRepo()
	conversations := newFakeConversationRepo()

	uc := NewStartConversationUseCase(
		newFakeInstanceRepo(instance),
		newFakeServerRepo(healthyServer("srv-a", 10, 1)),
		contacts,
		conversations,
		messaging,
		nil,
	)
	return uc, messaging, contacts, conversations
}

// The number is verified against WhatsApp BEFORE anything is written.
//
// This is the flow's most valuable step: messaging numbers that are not on
// WhatsApp is one of the strongest ban signals there is, and an operator pasting
// from a spreadsheet cannot know. Refusing costs one provider call; sending
// costs the number.
func TestStartConversationRefusesANumberNotOnWhatsApp(t *testing.T) {
	uc, messaging, contacts, conversations := startFixture(t, nil)
	messaging.CheckNumbersFn = func(context.Context, uw.InstanceRef, []string) ([]uw.NumberCheck, error) {
		return []uw.NumberCheck{{Query: "5511999999999", IsOnWhatsApp: false}}, nil
	}

	_, err := uc.Execute(context.Background(), StartConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-1", PhoneNumber: "5511999999999",
	})
	if !errors.Is(err, ErrNotOnWhatsApp) {
		t.Fatalf("err = %v, want ErrNotOnWhatsApp", err)
	}
	// Nothing may be persisted for a number we refused to reach, or the inbox
	// fills with conversations that can never receive a message.
	if len(contacts.created) != 0 {
		t.Errorf("%d contact(s) written for an unreachable number", len(contacts.created))
	}
	if len(conversations.created) != 0 {
		t.Errorf("%d conversation(s) written for an unreachable number", len(conversations.created))
	}
}

// The AUTHORITATIVE JID comes from the check, never from string concatenation.
//
// An account that has been migrated answers on a different JID than
// "<phone>@s.whatsapp.net", and addressing the constructed one silently opens a
// conversation with nobody.
func TestStartConversationUsesTheProvidersJID(t *testing.T) {
	uc, messaging, _, conversations := startFixture(t, nil)
	messaging.CheckNumbersFn = func(context.Context, uw.InstanceRef, []string) ([]uw.NumberCheck, error) {
		return []uw.NumberCheck{{
			Query: "5511999999999", JID: "5511000000000@s.whatsapp.net",
			LID: "189923456789012@lid", IsOnWhatsApp: true, VerifiedName: "Loja X",
		}}, nil
	}

	started, err := uc.Execute(context.Background(), StartConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-1", PhoneNumber: "+55 (11) 99999-9999",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(conversations.created) != 1 {
		t.Fatalf("expected one conversation, got %d", len(conversations.created))
	}
	if got := conversations.created[0].ChatID; got != "5511000000000@s.whatsapp.net" {
		t.Errorf("chat id = %q, want the JID the provider reported", got)
	}
	// The typed number is normalised, so a pasted "+55 (11) …" resolves to the
	// same contact an inbound message would.
	if started.PhoneNumber != "5511999999999" {
		t.Errorf("phone = %q, want it normalised", started.PhoneNumber)
	}
}

// A number that already has a conversation must reopen it, never duplicate it.
func TestStartConversationReopensAnExistingChat(t *testing.T) {
	uc, _, _, _ := startFixture(t, nil)
	in := StartConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-1", PhoneNumber: "5511999999999",
	}

	first, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	second, err := uc.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if first.ConversationID != second.ConversationID {
		t.Errorf("a second attempt opened a different conversation (%s vs %s)",
			first.ConversationID, second.ConversationID)
	}
	if first.AlreadyExisted {
		t.Error("the first attempt reported the conversation as pre-existing")
	}
	if !second.AlreadyExisted {
		t.Error("the second attempt must say it reopened rather than created")
	}
}

// A mistyped number is rejected before it costs a provider call.
func TestStartConversationRejectsAnUnusableNumber(t *testing.T) {
	uc, messaging, _, _ := startFixture(t, nil)
	called := false
	messaging.CheckNumbersFn = func(context.Context, uw.InstanceRef, []string) ([]uw.NumberCheck, error) {
		called = true
		return nil, nil
	}

	for _, raw := range []string{"", "123", "abc", "+55 11"} {
		if _, err := uc.Execute(context.Background(), StartConversationInput{
			WorkspaceID: "ws-1", InstanceID: "inst-1", PhoneNumber: raw,
		}); !errors.Is(err, ErrInvalidPhone) {
			t.Errorf("Execute(%q) err = %v, want ErrInvalidPhone", raw, err)
		}
	}
	if called {
		t.Error("a mistyped number reached the provider; it must be refused locally")
	}
}

// A disconnected number cannot start anything, and the refusal must name the
// reason rather than failing at the first send.
func TestStartConversationRefusesAnInstanceThatCannotSend(t *testing.T) {
	uc, _, _, _ := startFixture(t, func(i *uw.Instance) {
		i.Status = uw.StatusDisconnected
	})

	if _, err := uc.Execute(context.Background(), StartConversationInput{
		WorkspaceID: "ws-1", InstanceID: "inst-1", PhoneNumber: "5511999999999",
	}); err == nil {
		t.Fatal("a disconnected instance opened a conversation")
	}
}

// The workspace on the request must own the instance. The route's permission
// gate proves the caller may send from SOME instance, not from this one.
func TestStartConversationRefusesAnotherWorkspacesInstance(t *testing.T) {
	uc, _, _, conversations := startFixture(t, nil)

	_, err := uc.Execute(context.Background(), StartConversationInput{
		WorkspaceID: "ws-intruder", InstanceID: "inst-1", PhoneNumber: "5511999999999",
	})
	if !errors.Is(err, uw.ErrInstanceNotFound) {
		t.Fatalf("err = %v, want ErrInstanceNotFound", err)
	}
	if len(conversations.created) != 0 {
		t.Error("a cross-workspace request wrote a conversation")
	}
}
