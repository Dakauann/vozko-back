package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/callrouting"
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type handoffPhones map[string]*businessphone.WhatsAppBusinessPhoneNumber

func (p handoffPhones) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if phone, ok := p[id]; ok {
		return phone, nil
	}
	return nil, errors.New("phone not found")
}

type handoffEntries map[string]string

func (e handoffEntries) FindByNumberAndBusinessPhone(number, businessPhoneID string) (*wce.WhatsAppCampaignEntry, error) {
	if id, ok := e[businessPhoneID+"|"+number]; ok {
		return &wce.WhatsAppCampaignEntry{ID: id}, nil
	}
	return nil, wce.ErrEntryNotFound
}

type assignCall struct {
	by                                        shared.Person
	workspaceID, entryID, entryType, toUserID string
}

type personAssignSpy struct {
	checked, assigned []assignCall
	refuse            error
}

func (s *personAssignSpy) Assign(by shared.Person, workspaceID, entryID, entryType, toUserID string) error {
	s.assigned = append(s.assigned, assignCall{by, workspaceID, entryID, entryType, toUserID})
	return s.refuse
}

func (s *personAssignSpy) Check(by shared.Person, workspaceID, entryID, entryType, toUserID string) error {
	s.checked = append(s.checked, assignCall{by, workspaceID, entryID, entryType, toUserID})
	return s.refuse
}

func newHandoffFixture() (*personAssignSpy, callrouting.ConversationHandoff) {
	spy := &personAssignSpy{}
	handoff := NewCallConversationHandoff(CallConversationHandoffDeps{
		Phones:  handoffPhones{"bp1": {ID: "bp1", OwnerWorkspaceID: "ws1"}},
		Entries: handoffEntries{"bp1|5584994409684": "entry-1"},
		Assign:  spy,
	})
	return spy, handoff
}

var handover = callrouting.Handover{
	WorkspaceID: "ws1",
	Contact:     conversation_domain.CallContact{BusinessPhoneID: "bp1", ContactNumber: "5584994409684"},
	FromUserID:  "ana",
	ToUserID:    "bia",
}

func TestHandingOverACallAssignsItsConversationAsTheTransferringOperator(t *testing.T) {
	spy, handoff := newHandoffFixture()

	if err := handoff.MayHandOver(context.Background(), handover); err != nil {
		t.Fatalf("MayHandOver: %v", err)
	}
	if err := handoff.HandOver(context.Background(), handover); err != nil {
		t.Fatalf("HandOver: %v", err)
	}
	want := assignCall{shared.Person{UserID: "ana"}, "ws1", "entry-1", string(shared.EntryTypeWhatsApp), "bia"}
	if len(spy.checked) != 1 || spy.checked[0] != want {
		t.Fatalf("checked = %+v", spy.checked)
	}
	if len(spy.assigned) != 1 || spy.assigned[0] != want {
		t.Fatalf("assigned = %+v", spy.assigned)
	}
}

func TestAssignmentRulesDecideWhetherTheConversationMayFollow(t *testing.T) {
	spy, handoff := newHandoffFixture()
	spy.refuse = errors.New("target out of reach")

	if err := handoff.MayHandOver(context.Background(), handover); !errors.Is(err, spy.refuse) {
		t.Fatalf("err = %v", err)
	}
}

func TestACallWithoutAConversationCannotBeHandedOver(t *testing.T) {
	spy, handoff := newHandoffFixture()
	unknown := handover
	unknown.Contact.ContactNumber = "5511999999999"

	if err := handoff.MayHandOver(context.Background(), unknown); err == nil {
		t.Fatal("a call with no conversation was allowed to hand one over")
	}
	if len(spy.checked) != 0 {
		t.Fatal("assignment rules ran without a conversation")
	}
}

func TestAConversationOnAnotherWorkspacesNumberIsNeverHandedOver(t *testing.T) {
	spy, handoff := newHandoffFixture()
	foreign := handover
	foreign.WorkspaceID = "ws2"

	if err := handoff.HandOver(context.Background(), foreign); !errors.Is(err, ErrCallConversationNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(spy.assigned) != 0 {
		t.Fatal("a foreign conversation was reassigned")
	}
}

func TestAnUnknownNumberCannotBeHandedOver(t *testing.T) {
	_, handoff := newHandoffFixture()
	missing := handover
	missing.Contact.BusinessPhoneID = "bp9"

	if err := handoff.MayHandOver(context.Background(), missing); !errors.Is(err, ErrCallConversationNotFound) {
		t.Fatalf("err = %v", err)
	}
}
