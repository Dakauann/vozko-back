package whatsapp_outreach

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/lead"
	wo "vozko/domain/whatsapp_outreach"
)

type fakeClaims struct {
	mu       sync.Mutex
	held     map[string]bool
	err      error
	released []string
}

func newFakeClaims() *fakeClaims { return &fakeClaims{held: map[string]bool{}} }

func (c *fakeClaims) SetNX(key, _ string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return false, c.err
	}
	if c.held[key] {
		return false, nil
	}
	c.held[key] = true
	return true, nil
}

func (c *fakeClaims) Del(keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.held, key)
		c.released = append(c.released, key)
	}
	return nil
}

func (c *fakeClaims) holdEveryContact() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held["campaign:send-claim:bp-1:lead-1"] = true
}

func optedOutLead() *lead.Lead {
	at := conversationNow.Add(-time.Hour)
	return &lead.Lead{ID: "lead-1", Number: "5511999999999", OptedOutAt: &at, OptOutSource: lead.OptOutOperator}
}

func TestStartConversation_OptedOutLead_Refused(t *testing.T) {
	uc := newUC(t, func(d *Deps) { d.Leads = &fakeLeads{rec: optedOutLead()} })
	if _, err := uc.uc.Execute(context.Background(), input()); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("want ErrLeadOptedOut, got %v", err)
	}
	if uc.sender.calls != 0 || len(uc.entries.created) != 0 {
		t.Fatal("nothing may be charged or created to message a lead that asked not to receive messages")
	}
}

func TestConversationTemplate_OptedOutLead_RefusedOnCheckAndSend(t *testing.T) {
	f := newConversationUC(t, func(d *Deps) { d.Leads = &fakeLeads{rec: optedOutLead()} })
	if _, err := f.uc.Check(context.Background(), conversationInput()); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("check: want ErrLeadOptedOut, got %v", err)
	}
	if _, err := f.uc.Send(context.Background(), conversationInput()); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("send: want ErrLeadOptedOut, got %v", err)
	}
	if f.sender.calls != 0 {
		t.Fatal("an opted out lead must never be charged")
	}
}

func TestConversationTemplate_ContactWithoutANumber_Refused(t *testing.T) {
	f := newConversationUC(t, func(d *Deps) { d.Leads = &fakeLeads{rec: &lead.Lead{ID: "lead-1"}} })
	if _, err := f.uc.Send(context.Background(), conversationInput()); !errors.Is(err, wo.ErrInvalidPhone) {
		t.Fatalf("want ErrInvalidPhone, got %v", err)
	}
	if f.sender.calls != 0 {
		t.Fatal("a lead without a number must never be charged")
	}
}

func TestConversationTemplate_ContactHeldByAnotherSend_Refused(t *testing.T) {
	claims := newFakeClaims()
	claims.holdEveryContact()
	f := newConversationUC(t, func(d *Deps) { d.SendClaims = claims })
	if _, err := f.uc.Check(context.Background(), conversationInput()); err != nil {
		t.Fatalf("a preview takes no claim: %v", err)
	}
	if _, err := f.uc.Send(context.Background(), conversationInput()); !errors.Is(err, wo.ErrWithinSpamWindow) {
		t.Fatalf("want ErrWithinSpamWindow while another send holds the contact, got %v", err)
	}
	if f.sender.calls != 0 {
		t.Fatal("a contact being messaged by another send must not be charged again")
	}
}

func TestConversationTemplate_KeepsTheClaimAfterASendAndFreesItOnFailure(t *testing.T) {
	claims := newFakeClaims()
	f := newConversationUC(t, func(d *Deps) { d.SendClaims = claims })
	if _, err := f.uc.Send(context.Background(), conversationInput()); err != nil {
		t.Fatal(err)
	}
	if !claims.held["campaign:send-claim:bp-1:lead-1"] || len(claims.released) != 0 {
		t.Fatalf("held = %v released = %v, want the claim kept until the cooldown is on record", claims.held, claims.released)
	}

	claims = newFakeClaims()
	failing := newConversationUC(t, func(d *Deps) { d.SendClaims = claims })
	failing.sender.result, failing.sender.err = nil, errors.New("provider down")
	if _, err := failing.uc.Send(context.Background(), conversationInput()); err == nil {
		t.Fatal("the send failure must surface")
	}
	if len(claims.held) != 0 || len(claims.released) != 1 {
		t.Fatalf("held = %v released = %v, want a failed send to free the contact", claims.held, claims.released)
	}
}

func TestConversationTemplate_ClaimStoreDown_Refused(t *testing.T) {
	claims := newFakeClaims()
	claims.err = errors.New("redis down")
	f := newConversationUC(t, func(d *Deps) { d.SendClaims = claims })
	if _, err := f.uc.Send(context.Background(), conversationInput()); err == nil {
		t.Fatal("an unreachable claim store must refuse the send")
	}
	if f.sender.calls != 0 {
		t.Fatal("nothing may be charged without the claim")
	}
}

func TestNewStartConversationUseCase_RefusesWithoutTheSendClaims(t *testing.T) {
	cases := map[string]func(*Deps){
		"no claims": func(d *Deps) { d.SendClaims = nil },
	}
	for name, drop := range cases {
		t.Run(name, func(t *testing.T) {
			deps := startDeps()
			drop(&deps)
			if _, err := NewStartConversationUseCase(deps); err == nil {
				t.Fatalf("a use case with %s would start conversations unguarded", name)
			}
		})
	}
}

func startDeps() Deps {
	return Deps{
		Phones: &fakePhones{}, Templates: &fakeTemplates{}, TemplateGrant: &fakeGrant{},
		Leads: &fakeLeads{}, Entries: &fakeEntries{}, EnsureReceptive: &fakeOrganic{},
		CampaignSends: &fakeCampaignSends{}, SpamPolicy: &fakeSpamPolicy{}, SendClaims: newFakeClaims(),
		History: &fakeHistory{}, Assignments: &fakeClaimer{}, Sender: &fakeSender{},
	}
}

func TestNewStartConversationUseCase_BuildsWithEveryGuard(t *testing.T) {
	if _, err := NewStartConversationUseCase(startDeps()); err != nil {
		t.Fatalf("a fully wired use case must build: %v", err)
	}
}
