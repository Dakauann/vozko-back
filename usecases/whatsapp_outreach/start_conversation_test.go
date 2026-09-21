package whatsapp_outreach

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	wo "vozko/domain/whatsapp_outreach"
)

type fakePhones struct {
	businessphone.Repository
	phone *businessphone.WhatsAppBusinessPhoneNumber
}

func (f *fakePhones) FindByID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if f.phone == nil {
		return nil, errors.New("not found")
	}
	return f.phone, nil
}

type fakeTemplates struct {
	template.Repository
	tmpl *template.Template
}

func (f *fakeTemplates) FindByID(string) (*template.Template, error) { return f.tmpl, nil }

type fakeLeads struct {
	lead.Repository
	rec *lead.Lead
}

func (f *fakeLeads) FindOrCreate(string, string, lead.LeadUpdate) (*lead.Lead, bool, error) {
	return f.rec, false, nil
}

type fakeEntries struct {
	wce.Repository
	existing *wce.WhatsAppCampaignEntry
	created  []*wce.WhatsAppCampaignEntry
	statuses []string
}

func (f *fakeEntries) FindByNumberAndBusinessPhone(string, string) (*wce.WhatsAppCampaignEntry, error) {
	if f.existing == nil {
		return nil, wce.ErrEntryNotFound
	}
	return f.existing, nil
}
func (f *fakeEntries) FindByCampaignAndLead(string, string) (*wce.WhatsAppCampaignEntry, error) {
	return f.existing, nil
}
func (f *fakeEntries) Create(e *wce.WhatsAppCampaignEntry) error {
	f.created = append(f.created, e)
	return nil
}
func (f *fakeEntries) UpdateStatus(_ string, status wce.SendStatus, _ string, _ int, _ string) error {
	f.statuses = append(f.statuses, string(status))
	return nil
}
func (f *fakeEntries) UpdateMetadata(string, map[string]interface{}) error { return nil }

type fakeOrganic struct{ campaign *wc.Campaign }

func (f *fakeOrganic) Execute(string, string, string) (*wc.Campaign, bool, error) {
	return f.campaign, false, nil
}

type fakeWindows struct {
	open bool
	err  error
}

func (f *fakeWindows) IsWindowOpen(string, string) (bool, error) { return f.open, f.err }
func (f *fakeWindows) RecordMessage(string, string) (*windowRow, error) {
	panic("a template send must never open the window: Meta opens it on the customer's reply")
}

type fakeSender struct {
	calls  int
	result *template.BilledSendResult
	err    error
	lastIn template.BilledSendInput
}

func (f *fakeSender) Execute(_ context.Context, in template.BilledSendInput) (*template.BilledSendResult, error) {
	f.calls++
	f.lastIn = in
	return f.result, f.err
}

type fakeHistory struct {
	records []conversation.MessageHistoryRecord
	err     error
}

func (f *fakeHistory) Record(_ context.Context, _ conversation.MessageHistoryDirection, r conversation.MessageHistoryRecord) error {
	if f.err != nil {
		return f.err
	}
	f.records = append(f.records, r)
	return nil
}

type h struct {
	uc      wo.StartOfficialConversationUseCase
	entries *fakeEntries
	sender  *fakeSender
	history *fakeHistory
	windows *fakeWindows
}

func newUC(t *testing.T, mutate ...func(*Deps)) *h {
	t.Helper()
	entries := &fakeEntries{}
	sender := &fakeSender{result: &template.BilledSendResult{
		AttemptID: "att-1", Status: template.SendAttemptSent, MessageID: "wamid.1", ChargedMicros: 5000,
	}}
	history := &fakeHistory{}
	windows := &fakeWindows{}

	deps := Deps{
		Phones: &fakePhones{phone: &businessphone.WhatsAppBusinessPhoneNumber{
			ID: "bp-1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1",
			DisplayPhoneNumber: "5511888888888", Status: businessphone.StatusConnected,
		}},
		Templates: &fakeTemplates{tmpl: &template.Template{
			ID: "tpl-1", Name: "aviso", Language: "pt_BR",
			Category: template.TemplateCategoryUtility, Status: template.TemplateStatusApproved,
		}},
		Leads:         &fakeLeads{rec: &lead.Lead{ID: "lead-1", Number: "5511999999999"}},
		Entries:       entries,
		EnsureOrganic: &fakeOrganic{campaign: &wc.Campaign{ID: "camp-1", WorkspaceID: "ws-1", Type: wc.CampaignTypeOrganic}},
		Windows:       nil,
		History:       history,
		Sender:        sender,
	}
	_ = windows
	for _, m := range mutate {
		m(&deps)
	}
	uc, err := NewStartConversationUseCase(deps)
	if err != nil {
		t.Fatalf("constructor: %v", err)
	}
	return &h{uc: uc, entries: entries, sender: sender, history: history, windows: windows}
}

func input() wo.StartConversationInput {
	return wo.StartConversationInput{
		WorkspaceID:     "ws-1",
		UserID:          "user-1",
		BusinessPhoneID: "bp-1",
		TemplateID:      "tpl-1",
		PhoneNumber:     "5511999999999",
		IdempotencyKey:  "key-1",
	}
}

func TestStartConversation_CreatesEntryAndMessageTogether(t *testing.T) {
	uc := newUC(t)

	result, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(uc.entries.created) != 1 {
		t.Fatalf("created %d entries, want 1", len(uc.entries.created))
	}
	if uc.entries.created[0].Status != wce.SendStatusPending {
		t.Fatal("an entry must start PENDING: SENT is counted as a billed dispatch")
	}
	if len(uc.history.records) != 1 {
		t.Fatal("without a message row the conversation never appears in the inbox")
	}
	if !result.Recorded {
		t.Fatal("result must report the message was recorded")
	}
	if result.EntryID != uc.entries.created[0].ID {
		t.Fatal("the caller must be handed the entry id the inbox addresses")
	}
}

func TestStartConversation_MessageCarriesTheOperator(t *testing.T) {
	uc := newUC(t)
	if _, err := uc.uc.Execute(context.Background(), input()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := uc.history.records[0].From; got != "user-1" {
		t.Fatalf("message From = %q, want the operator", got)
	}
}

func TestStartConversation_PersistFailureStillReportsSuccess(t *testing.T) {
	uc := newUC(t, func(d *Deps) {
		d.History = &fakeHistory{err: errors.New("database is on fire")}
	})

	result, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatalf("a delivered, paid send must not surface as an error: %v", err)
	}
	if result.Recorded {
		t.Fatal("the result must admit the message was not recorded")
	}
	if result.MessageID == "" {
		t.Fatal("the send still happened and its id must come back")
	}
}

func TestStartConversation_OpenWindow_RefusesAndPointsAtTheConversation(t *testing.T) {
	existing := &wce.WhatsAppCampaignEntry{ID: "entry-9", CampaignID: "camp-1", LeadID: "lead-1"}
	uc := newUC(t, func(d *Deps) {
		d.Windows = &fakeWindows{open: true}
		d.Entries = &fakeEntries{existing: existing}
	})

	result, err := uc.uc.Execute(context.Background(), input())
	if !errors.Is(err, wo.ErrWindowAlreadyOpen) {
		t.Fatalf("want ErrWindowAlreadyOpen, got %v", err)
	}
	if uc.sender.calls != 0 {
		t.Fatal("nothing may be charged when a free reply is possible")
	}
	if result == nil || result.EntryID != "entry-9" {
		t.Fatalf("the refusal must carry the conversation to open, got %+v", result)
	}
}

func TestStartConversation_WindowReadFails_FailsClosed(t *testing.T) {
	uc := newUC(t, func(d *Deps) {
		d.Windows = &fakeWindows{err: errors.New("redis down")}
	})

	if _, err := uc.uc.Execute(context.Background(), input()); err == nil {
		t.Fatal("an unreadable window must refuse the send, not proceed")
	}
	if uc.sender.calls != 0 {
		t.Fatal("nothing may be charged")
	}
}

func TestStartConversation_UnnormalisableNumber_RefusedBeforeAnything(t *testing.T) {
	uc := newUC(t)
	in := input()
	in.PhoneNumber = "12"

	if _, err := uc.uc.Execute(context.Background(), in); !errors.Is(err, wo.ErrInvalidPhone) {
		t.Fatalf("want ErrInvalidPhone, got %v", err)
	}
	if uc.sender.calls != 0 {
		t.Fatal("nothing may be charged for an unusable number")
	}
}

func TestStartConversation_ForeignPhone_RefusedAsNotFound(t *testing.T) {
	uc := newUC(t, func(d *Deps) {
		d.Phones = &fakePhones{phone: &businessphone.WhatsAppBusinessPhoneNumber{
			ID: "bp-1", OwnerWorkspaceID: "someone-else", Status: businessphone.StatusConnected,
		}}
	})

	if _, err := uc.uc.Execute(context.Background(), input()); !errors.Is(err, wo.ErrBusinessPhoneNotFound) {
		t.Fatalf("want ErrBusinessPhoneNotFound, got %v", err)
	}
	if uc.sender.calls != 0 {
		t.Fatal("nothing may be charged against another workspace's number")
	}
}

func TestStartConversation_BlockedLead_Refused(t *testing.T) {
	uc := newUC(t, func(d *Deps) {
		d.Leads = &fakeLeads{rec: &lead.Lead{ID: "lead-1", Number: "5511999999999", Blocked: true}}
	})

	if _, err := uc.uc.Execute(context.Background(), input()); !errors.Is(err, wo.ErrLeadBlocked) {
		t.Fatalf("want ErrLeadBlocked, got %v", err)
	}
	if uc.sender.calls != 0 {
		t.Fatal("nothing may be charged to message a blocked contact")
	}
}

func TestStartConversation_ExistingConversation_IsReused(t *testing.T) {
	existing := &wce.WhatsAppCampaignEntry{ID: "entry-7", CampaignID: "camp-1", LeadID: "lead-1"}
	uc := newUC(t, func(d *Deps) { d.Entries = &fakeEntries{existing: existing} })

	result, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if result.EntryID != "entry-7" || !result.ConversationExisted {
		t.Fatalf("existing conversation not reused: %+v", result)
	}
}

func TestStartConversation_Replay_WritesNothingTwice(t *testing.T) {
	uc := newUC(t, func(d *Deps) {})
	uc.sender.result = &template.BilledSendResult{
		AttemptID: "att-1", Status: template.SendAttemptSent, MessageID: "wamid.1", Replayed: true,
	}

	result, err := uc.uc.Execute(context.Background(), input())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !result.Replayed {
		t.Fatal("the result must report the replay")
	}
	if len(uc.history.records) != 0 {
		t.Fatal("a replay must not write a second message into the thread")
	}
}

func TestStartConversation_SendsAddressingFormKeepsStoredForm(t *testing.T) {
	uc := newUC(t)
	if _, err := uc.uc.Execute(context.Background(), input()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if uc.sender.lastIn.ToNumber != lead.NormalizeWhatsAppNumber("5511999999999") {
		t.Fatalf("provider was addressed with %q", uc.sender.lastIn.ToNumber)
	}
	if uc.sender.lastIn.CampaignID != "camp-1" || uc.sender.lastIn.EntryID == "" {
		t.Fatal("the charge must carry its container so reporting can attribute it")
	}
	if uc.sender.lastIn.IdempotencyKey != "key-1" {
		t.Fatal("the caller's key must reach the sender or retries charge twice")
	}
}

func TestStartConversation_SendRejected_MarksEntryFailed(t *testing.T) {
	uc := newUC(t)
	uc.sender.result = &template.BilledSendResult{AttemptID: "att-1", Outcome: template.OutcomeRejected}
	uc.sender.err = errors.New("template paused")

	if _, err := uc.uc.Execute(context.Background(), input()); err == nil {
		t.Fatal("a rejected send must surface as an error")
	}
	if len(uc.entries.statuses) == 0 || uc.entries.statuses[0] != string(wce.SendStatusFailed) {
		t.Fatalf("entry statuses = %v, want a FAILED", uc.entries.statuses)
	}
}

func TestStartConversation_UnknownOutcome_LeavesEntryPending(t *testing.T) {
	uc := newUC(t)
	uc.sender.result = &template.BilledSendResult{AttemptID: "att-1", Outcome: template.OutcomeUnknown}
	uc.sender.err = errors.New("upstream down")

	if _, err := uc.uc.Execute(context.Background(), input()); err == nil {
		t.Fatal("an unknown outcome still surfaces to the caller")
	}
	if len(uc.entries.statuses) != 0 {
		t.Fatalf("entry must be left alone, got statuses %v", uc.entries.statuses)
	}
}

func TestNewStartConversationUseCase_RefusesWithoutSender(t *testing.T) {
	_, err := NewStartConversationUseCase(Deps{
		Phones:        &fakePhones{},
		Templates:     &fakeTemplates{},
		Leads:         &fakeLeads{},
		Entries:       &fakeEntries{},
		EnsureOrganic: &fakeOrganic{},
	})
	if err == nil {
		t.Fatal("a use case with no sender would create conversations nobody receives a message in")
	}
}

type windowRow = struct{}

var _ = time.Now
