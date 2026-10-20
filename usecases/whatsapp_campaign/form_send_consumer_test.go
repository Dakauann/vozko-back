package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wf "vozko/domain/whatsappform"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type consumerFormSends struct {
	mu       sync.Mutex
	prepared []whatsappform_usecase.TemplateSend
	settled  []consumerSettlement
	err      error
}

type consumerSettlement struct {
	token     string
	messageID string
	err       error
}

func (f *consumerFormSends) PrepareTemplateSend(_ context.Context, in whatsappform_usecase.TemplateSend) (wf.Send, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared = append(f.prepared, in)
	if f.err != nil {
		return wf.Send{}, f.err
	}
	return wf.Send{Token: "flowtok_c", Status: wf.SendPrepared}, nil
}

func (f *consumerFormSends) SettleTemplateSend(_ context.Context, token, messageID string, sendErr error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled = append(f.settled, consumerSettlement{token: token, messageID: messageID, err: sendErr})
	return nil
}

type recordingTemplateClient struct {
	*mockWhatsAppClient
	mu   sync.Mutex
	last []conversation.SendTemplateMessageInput
	out  *conversation.SendTextMessageOutput
	err  error
}

func (c *recordingTemplateClient) SendTemplateMessage(_ context.Context, in conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = append(c.last, in)
	if c.out == nil && c.err == nil {
		return &conversation.SendTextMessageOutput{MessageID: "wamid-form-1"}, nil
	}
	return c.out, c.err
}

type recordingClientFactory struct{ client conversation.WhatsAppClient }

func (f recordingClientFactory) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}
func (f recordingClientFactory) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return f.client, nil
}
func (f recordingClientFactory) WABAIdForPhone(string) (string, error) { return "waba-1", nil }

func formCampaignTemplate() *template.Template {
	return &template.Template{
		ID: "tmpl-1", Name: "cadastro", Language: "pt_BR", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryMarketing,
		Components: []template.TemplateComponent{
			{Type: "BODY", Text: "Responda o cadastro"},
			{Type: "BUTTONS", Buttons: []template.TemplateButton{{Type: template.ButtonTypeFlow, Text: "Responder", FlowID: "F1", FlowAction: template.FlowActionNavigate, NavigateScreen: "S_INICIO"}}},
		},
	}
}

type formConsumerRig struct {
	h      *testHarness
	client *recordingTemplateClient
	forms  *consumerFormSends
	topic  string
}

func newFormConsumerRig(campID string, tmpl *template.Template, forms *consumerFormSends, mutate ...func(*wc.Campaign)) *formConsumerRig {
	h := newTestHarness()
	client := &recordingTemplateClient{mockWhatsAppClient: h.waClient}
	h.consumer.WhatsAppClientFactory = recordingClientFactory{client: client}
	if forms != nil {
		h.consumer.SetFormSends(forms)
	}
	topic := setupCampaignWithCounter(h, campID, 1)
	h.templateRepo.templates["tmpl-1"] = tmpl
	camp, _ := h.campaignRepo.FindByID(campID)
	for _, m := range mutate {
		m(camp)
	}
	h.entryRepo.entries["entry-1"] = &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: campID, LeadID: "lead-1", Status: wce.SendStatusPending}
	return &formConsumerRig{h: h, client: client, forms: forms, topic: topic}
}

func (r *formConsumerRig) deliver(t *testing.T, campID string) {
	t.Helper()
	if ack := r.h.queueSub.deliver(r.topic, makePayload(campID, "entry-1", "5584999990001")); ack == nil || !ack.acked.Load() {
		t.Fatal("the entry must be settled and acked")
	}
}

func TestCampaignTemplateWithoutAFormNeverPrepares(t *testing.T) {
	forms := &consumerFormSends{}
	rig := newFormConsumerRig("camp-plain", approvedMarketingTemplate("tmpl-1"), forms)

	rig.deliver(t, "camp-plain")

	if len(forms.prepared) != 0 || len(forms.settled) != 0 {
		t.Fatalf("prepared %d settled %d, want nothing for a template without a form", len(forms.prepared), len(forms.settled))
	}
	if status := rig.h.entryRepo.getStatus("entry-1"); status != wce.SendStatusSent {
		t.Fatalf("status = %s, want SENT", status)
	}
}

func TestCampaignFormTemplatePreparesAndSettles(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		wantOrigin wf.SendOrigin
	}{
		{name: "campaign", wantOrigin: wf.OriginCampaign},
		{name: "bulk send from a lead selection", source: campaign.SourceLeadSelection, wantOrigin: wf.OriginTemplateBulk},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forms := &consumerFormSends{}
			rig := newFormConsumerRig("camp-form", formCampaignTemplate(), forms, func(c *wc.Campaign) { c.Source = tc.source })

			rig.deliver(t, "camp-form")

			want := whatsappform_usecase.TemplateSend{
				WorkspaceID: "ws-1", BusinessPhoneID: "bp-1", ContactNumber: "5584999990001", MetaFlowID: "F1", TemplateID: "tmpl-1",
				Origin: tc.wantOrigin, OriginRef: "camp-form", EntryID: "entry-1", LeadID: "lead-1",
			}
			if len(forms.prepared) != 1 || forms.prepared[0] != want {
				t.Fatalf("prepared %+v, want %+v", forms.prepared, want)
			}
			if len(rig.client.last) != 1 {
				t.Fatalf("sends = %d, want 1", len(rig.client.last))
			}
			if buttons := rig.client.last[0].Buttons; len(buttons) != 1 || buttons[0].FlowToken != "flowtok_c" {
				t.Fatalf("buttons %+v, want the prepared token", buttons)
			}
			if len(forms.settled) != 1 || forms.settled[0] != (consumerSettlement{token: "flowtok_c", messageID: "wamid-form-1"}) {
				t.Fatalf("settled %+v", forms.settled)
			}
			if status := rig.h.entryRepo.getStatus("entry-1"); status != wce.SendStatusSent {
				t.Fatalf("status = %s, want SENT", status)
			}
			if rig.h.consumeTempl.consumed.Load() != 1 || rig.h.consumeTempl.refunded.Load() != 0 {
				t.Fatalf("consumed %d refunded %d", rig.h.consumeTempl.consumed.Load(), rig.h.consumeTempl.refunded.Load())
			}
		})
	}
}

func TestCampaignFormThatCannotBeSentFailsTheEntryAndRefunds(t *testing.T) {
	cases := []struct {
		name  string
		forms *consumerFormSends
	}{
		{name: "publication blocked by Meta", forms: &consumerFormSends{err: wf.ErrPublicationNotSendable}},
		{name: "form not published on the account", forms: &consumerFormSends{err: wf.ErrNoPublication}},
		{name: "sends unreadable", forms: &consumerFormSends{err: errors.New("db down")}},
		{name: "form sends not wired"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newFormConsumerRig("camp-refused", formCampaignTemplate(), tc.forms)

			rig.deliver(t, "camp-refused")

			if len(rig.client.last) != 0 {
				t.Fatalf("sends = %d, a form that cannot be sent must not reach Meta", len(rig.client.last))
			}
			if status := rig.h.entryRepo.getStatus("entry-1"); status != wce.SendStatusFailed {
				t.Fatalf("status = %s, want FAILED", status)
			}
			if code := rig.h.entryRepo.getErrorCode("entry-1"); code != wce.ErrorCodeFormNotSendable {
				t.Fatalf("code = %d, want %d", code, wce.ErrorCodeFormNotSendable)
			}
			if rig.h.entryRepo.getErrorMessage("entry-1") == "" {
				t.Fatal("the failure must say why")
			}
			if consumed, refunded := rig.h.consumeTempl.consumed.Load(), rig.h.consumeTempl.refunded.Load(); consumed != refunded {
				t.Fatalf("consumed %d refunded %d, nothing may stay charged", consumed, refunded)
			}
			if tc.forms != nil && len(tc.forms.settled) != 0 {
				t.Fatalf("settled %+v, a refused preparation has nothing to settle", tc.forms.settled)
			}
		})
	}
}

func TestCampaignFormSendIsSettledWhenTheProviderFails(t *testing.T) {
	cases := []struct {
		name         string
		out          *conversation.SendTextMessageOutput
		err          error
		wantStatus   wce.SendStatus
		wantRefunded int32
	}{
		{name: "provider rejected", out: &conversation.SendTextMessageOutput{ResponseStatus: 400}, err: errors.New("meta said no"), wantStatus: wce.SendStatusFailed, wantRefunded: 1},
		{name: "outcome unknown", err: conversation.ErrSendOutcomeUnknown, wantStatus: wce.SendStatusSent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forms := &consumerFormSends{}
			rig := newFormConsumerRig("camp-provider", formCampaignTemplate(), forms)
			rig.client.out, rig.client.err = tc.out, tc.err

			rig.deliver(t, "camp-provider")

			if len(forms.settled) != 1 || forms.settled[0].token != "flowtok_c" || !errors.Is(forms.settled[0].err, tc.err) {
				t.Fatalf("settled %+v, want the token with the provider error", forms.settled)
			}
			if status := rig.h.entryRepo.getStatus("entry-1"); status != tc.wantStatus {
				t.Fatalf("status = %s, want %s", status, tc.wantStatus)
			}
			if refunded := rig.h.consumeTempl.refunded.Load(); refunded != tc.wantRefunded {
				t.Fatalf("refunded %d, want %d", refunded, tc.wantRefunded)
			}
		})
	}
}
