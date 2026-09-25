package whatsapp_outreach

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
)

func (f *fakeEntries) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	if f.existing == nil || f.existing.ID != id {
		return nil, wce.ErrEntryNotFound
	}
	return f.existing, nil
}

func (f *fakeLeads) FindByID(_, id string) (*lead.Lead, error) {
	if f.rec == nil || f.rec.ID != id {
		return nil, lead.ErrLeadNotFound
	}
	return f.rec, nil
}

type fakeCampaigns struct {
	wc.Repository
	campaign *wc.Campaign
}

func (f *fakeCampaigns) FindByID(id string) (*wc.Campaign, error) {
	if f.campaign == nil || f.campaign.ID != id {
		return nil, wc.ErrCampaignNotFound
	}
	return f.campaign, nil
}

type fakeGrant struct {
	granted bool
	err     error
}

func (f *fakeGrant) Execute(string, string) (bool, error) { return f.granted, f.err }

type fakeSpamPolicy struct {
	days int
	err  error
}

func (f *fakeSpamPolicy) SpamProtectionDays(context.Context, string) (int, error) {
	return f.days, f.err
}

type fakeCampaignSends struct {
	lastSent *time.Time
	err      error
	recorded []string
}

func (f *fakeCampaignSends) Record(leadID, _, _ string) error {
	f.recorded = append(f.recorded, leadID)
	return nil
}
func (f *fakeCampaignSends) GetLastSendTime(string, string) (*time.Time, error) {
	return f.lastSent, f.err
}
func (f *fakeCampaignSends) GetLastSendTimesBatch([]string, string) (map[string]time.Time, error) {
	return nil, nil
}

type conversationFixture struct {
	uc      wo.ConversationTemplateUseCase
	entries *fakeEntries
	sender  *fakeSender
	history *fakeHistory
	sends   *fakeCampaignSends
}

var conversationNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func newConversationUC(t *testing.T, mutate ...func(*Deps)) *conversationFixture {
	t.Helper()
	entries := &fakeEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: "camp-1", LeadID: "lead-1"}}
	sender := &fakeSender{result: &template.BilledSendResult{
		AttemptID: "att-1", Status: template.SendAttemptSent, MessageID: "wamid.1", ChargedMicros: 5000,
	}}
	history := &fakeHistory{}
	sends := &fakeCampaignSends{}

	deps := Deps{
		Phones: &fakePhones{phone: &businessphone.WhatsAppBusinessPhoneNumber{
			ID: "bp-1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1", Status: businessphone.StatusConnected,
		}},
		Templates: &fakeTemplates{tmpl: &template.Template{
			ID: "tpl-1", Name: "follow_up", Language: "pt_BR", WABAId: "waba-1",
			Category: template.TemplateCategoryUtility, Status: template.TemplateStatusApproved,
			Components: []template.TemplateComponent{{Type: "BODY", Text: "Oi {{1}}, tudo certo?"}},
		}},
		TemplateGrant: &fakeGrant{granted: true},
		Leads:         &fakeLeads{rec: &lead.Lead{ID: "lead-1", Number: "5511999999999"}},
		Entries:       entries,
		Campaigns:     &fakeCampaigns{campaign: &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", BusinessPhoneID: "bp-1"}},
		CampaignSends: sends,
		SpamPolicy:    &fakeSpamPolicy{days: 3},
		History:       history,
		Sender:        sender,
		Now:           func() time.Time { return conversationNow },
	}
	for _, m := range mutate {
		m(&deps)
	}
	uc, err := NewConversationTemplateUseCase(deps)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	return &conversationFixture{uc: uc, entries: entries, sender: sender, history: history, sends: sends}
}

func conversationInput() wo.ConversationTemplateInput {
	return wo.ConversationTemplateInput{
		WorkspaceID:    "ws-1",
		UserID:         "user-1",
		EntryID:        "entry-1",
		TemplateID:     "tpl-1",
		BodyParams:     []string{"Ana"},
		IdempotencyKey: "sched-1",
	}
}

func TestConversationTemplate_CheckPreviewsWithoutCharging(t *testing.T) {
	f := newConversationUC(t)

	preview, err := f.uc.Check(context.Background(), conversationInput())
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if preview.Name != "follow_up" || preview.Preview != "Oi Ana, tudo certo?" {
		t.Fatalf("preview = %+v", preview)
	}
	if f.sender.calls != 0 {
		t.Fatal("a check must never charge")
	}
}

func TestConversationTemplate_SendChargesOnceAndRecords(t *testing.T) {
	f := newConversationUC(t)

	sent, err := f.uc.Send(context.Background(), conversationInput())
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	in := f.sender.lastIn
	if in.IdempotencyKey != "sched-1" {
		t.Fatal("the caller's key must reach the billed sender or a redelivery charges twice")
	}
	if in.BusinessPhoneID != "bp-1" || in.EntryID != "entry-1" || in.CampaignID != "camp-1" {
		t.Fatalf("the charge must carry its conversation, got %+v", in)
	}
	if in.ToNumber != lead.NormalizeWhatsAppNumber("5511999999999") {
		t.Fatalf("provider was addressed with %q", in.ToNumber)
	}
	if len(f.history.records) != 1 || f.history.records[0].From != "user-1" {
		t.Fatalf("the template must appear in the thread as the operator's, got %+v", f.history.records)
	}
	if len(f.sends.recorded) != 1 {
		t.Fatal("the send must count against the spam window like a campaign send")
	}
	if !sent.Recorded || sent.ChargedMicros != 5000 {
		t.Fatalf("result = %+v", sent)
	}
}

func TestConversationTemplate_ReplayWritesNothingTwice(t *testing.T) {
	f := newConversationUC(t)
	f.sender.result = &template.BilledSendResult{AttemptID: "att-1", MessageID: "wamid.1", Replayed: true}

	sent, err := f.uc.Send(context.Background(), conversationInput())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !sent.Replayed || len(f.history.records) != 0 || len(f.sends.recorded) != 0 {
		t.Fatalf("a replay must not write again: %+v", sent)
	}
}

func TestConversationTemplate_RefusesBeforeCharging(t *testing.T) {
	past := conversationNow.Add(-24 * time.Hour)

	cases := []struct {
		name    string
		mutate  func(*Deps)
		input   func(*wo.ConversationTemplateInput)
		wantErr error
	}{
		{
			name:    "another workspace's conversation",
			mutate:  func(d *Deps) { d.Campaigns = &fakeCampaigns{campaign: &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-2", BusinessPhoneID: "bp-1"}} },
			wantErr: wo.ErrConversationNotFound,
		},
		{
			name:    "a conversation that does not exist",
			input:   func(in *wo.ConversationTemplateInput) { in.EntryID = "entry-404" },
			wantErr: wo.ErrConversationNotFound,
		},
		{
			name: "a number the workspace may not send from",
			mutate: func(d *Deps) {
				d.Phones = &fakePhones{phone: &businessphone.WhatsAppBusinessPhoneNumber{
					ID: "bp-1", OwnerWorkspaceID: "ws-2", Status: businessphone.StatusConnected,
				}}
			},
			wantErr: wo.ErrBusinessPhoneNotFound,
		},
		{
			name: "a disconnected number",
			mutate: func(d *Deps) {
				d.Phones = &fakePhones{phone: &businessphone.WhatsAppBusinessPhoneNumber{
					ID: "bp-1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1", Status: businessphone.StatusDisconnected,
				}}
			},
			wantErr: wo.ErrPhoneNotConnected,
		},
		{
			name:    "a template the workspace was not granted",
			mutate:  func(d *Deps) { d.TemplateGrant = &fakeGrant{granted: false} },
			wantErr: wo.ErrTemplateForbidden,
		},
		{
			name: "a paused template",
			mutate: func(d *Deps) {
				d.Templates = &fakeTemplates{tmpl: &template.Template{ID: "tpl-1", Name: "x", Status: template.TemplateStatusPaused}}
			},
			wantErr: template.ErrTemplateNotSendable,
		},
		{
			name: "a template of another business account",
			mutate: func(d *Deps) {
				d.Templates = &fakeTemplates{tmpl: &template.Template{
					ID: "tpl-1", Name: "x", WABAId: "waba-2", Status: template.TemplateStatusApproved,
					Components: []template.TemplateComponent{{Type: "BODY", Text: "Oi {{1}}"}},
				}}
			},
			wantErr: template.ErrTemplatePhoneMismatch,
		},
		{
			name:    "a variable left empty",
			input:   func(in *wo.ConversationTemplateInput) { in.BodyParams = nil },
			wantErr: template.ErrTemplateParamsMismatch,
		},
		{
			name:    "a blocked contact",
			mutate:  func(d *Deps) { d.Leads = &fakeLeads{rec: &lead.Lead{ID: "lead-1", Number: "5511999999999", Blocked: true}} },
			wantErr: wo.ErrLeadBlocked,
		},
		{
			name:    "a contact inside the spam window",
			mutate:  func(d *Deps) { d.CampaignSends = &fakeCampaignSends{lastSent: &past} },
			wantErr: wo.ErrWithinSpamWindow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mutations []func(*Deps)
			if tc.mutate != nil {
				mutations = append(mutations, tc.mutate)
			}
			f := newConversationUC(t, mutations...)
			in := conversationInput()
			if tc.input != nil {
				tc.input(&in)
			}

			if _, err := f.uc.Check(context.Background(), in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("check err = %v, want %v", err, tc.wantErr)
			}
			if _, err := f.uc.Send(context.Background(), in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("send err = %v, want %v", err, tc.wantErr)
			}
			if f.sender.calls != 0 {
				t.Fatal("nothing may be charged for a refused send")
			}
		})
	}
}

func TestConversationTemplate_UnreadableSpamHistoryFailsClosed(t *testing.T) {
	cases := map[string]func(*Deps){
		"the policy":   func(d *Deps) { d.SpamPolicy = &fakeSpamPolicy{err: errors.New("db down")} },
		"the last send": func(d *Deps) { d.CampaignSends = &fakeCampaignSends{err: errors.New("db down")} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newConversationUC(t, mutate)

			if _, err := f.uc.Send(context.Background(), conversationInput()); err == nil {
				t.Fatal("an unreadable spam window must refuse the send, not wave it through")
			}
			if f.sender.calls != 0 {
				t.Fatal("nothing may be charged")
			}
		})
	}
}

func TestConversationTemplate_RejectedSendSurfaces(t *testing.T) {
	f := newConversationUC(t)
	f.sender.result = &template.BilledSendResult{AttemptID: "att-1", Outcome: template.OutcomeRejected}
	f.sender.err = errors.New("template paused by Meta")

	if _, err := f.uc.Send(context.Background(), conversationInput()); err == nil {
		t.Fatal("a rejected send must surface")
	}
	if len(f.history.records) != 0 {
		t.Fatal("a rejected template must not appear in the thread")
	}
}

func TestNewConversationTemplateUseCase_RefusesMissingGuards(t *testing.T) {
	cases := map[string]func(*Deps){
		"template grant": func(d *Deps) { d.TemplateGrant = nil },
		"spam policy":    func(d *Deps) { d.SpamPolicy = nil },
		"campaign sends": func(d *Deps) { d.CampaignSends = nil },
		"campaigns":      func(d *Deps) { d.Campaigns = nil },
		"sender":         func(d *Deps) { d.Sender = nil },
	}
	for name, drop := range cases {
		t.Run(name, func(t *testing.T) {
			deps := Deps{
				Phones: &fakePhones{}, Templates: &fakeTemplates{}, TemplateGrant: &fakeGrant{},
				Leads: &fakeLeads{}, Entries: &fakeEntries{}, Campaigns: &fakeCampaigns{},
				CampaignSends: &fakeCampaignSends{}, SpamPolicy: &fakeSpamPolicy{},
				History: &fakeHistory{}, Sender: &fakeSender{},
			}
			drop(&deps)
			if _, err := NewConversationTemplateUseCase(deps); err == nil {
				t.Fatalf("a use case without its %s would send unguarded", name)
			}
		})
	}
}

func TestConversationTemplate_UnknownOutcomeIsNamed(t *testing.T) {
	f := newConversationUC(t)
	upstream := errors.New("connection reset")
	f.sender.result = &template.BilledSendResult{AttemptID: "att-1", Outcome: template.OutcomeUnknown}
	f.sender.err = upstream

	_, err := f.uc.Send(context.Background(), conversationInput())
	if !errors.Is(err, wo.ErrSendOutcomeUnknown) {
		t.Fatalf("err = %v, want the caller told the template may have gone out", err)
	}
	if !errors.Is(err, upstream) {
		t.Fatal("the provider's own error must stay in the chain")
	}
}
