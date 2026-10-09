package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/campaignguard"
)

type allowGrants struct{}

func (allowGrants) HasAccess(string, string) (bool, error) { return true, nil }

type screenCall struct {
	workspaceID string
	leadIDs     []string
	senderID    string
}

type fakeScreener struct {
	skip     map[string]campaign.SkipReason
	days     int
	err      error
	calls    []screenCall
	claimErr error
	claims   int
	released int
}

func (s *fakeScreener) Claim(context.Context, string, string, string) (func(), error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	s.claims++
	return func() { s.released++ }, nil
}

func (s *fakeScreener) Check(_ context.Context, workspaceID, leadID, senderID string) (campaign.SkipReason, error) {
	s.calls = append(s.calls, screenCall{workspaceID: workspaceID, leadIDs: []string{leadID}, senderID: senderID})
	if s.err != nil {
		return "", s.err
	}
	return s.skip[leadID], nil
}

func (s *fakeScreener) Screen(_ context.Context, workspaceID string, leadIDs []string, senderID string) (campaignguard.Screening, error) {
	s.calls = append(s.calls, screenCall{workspaceID: workspaceID, leadIDs: append([]string(nil), leadIDs...), senderID: senderID})
	if s.err != nil {
		return campaignguard.Screening{}, s.err
	}
	out := map[string]campaign.SkipReason{}
	for _, id := range leadIDs {
		if reason, ok := s.skip[id]; ok {
			out[id] = reason
		}
	}
	return campaignguard.Screening{Skipped: out, CooldownDays: s.days}, nil
}

type capturingEntries struct {
	*mockEntryRepo
	created []wce.WhatsAppCampaignEntry
	updates int
}

func (r *capturingEntries) CreateMany(entries []wce.WhatsAppCampaignEntry) ([]wce.WhatsAppCampaignEntry, error) {
	r.created = append(r.created, entries...)
	return entries, nil
}

func (r *capturingEntries) UpdateStatus(string, wce.SendStatus, string, int, string) error {
	r.updates++
	return nil
}

type numberedLeads struct {
	*qsLeadRepo
	resolved int
}

func (r *numberedLeads) FindOrCreateMany(_ string, inputs []lead.BulkLeadInput) (map[string]*lead.Lead, error) {
	r.resolved++
	out := map[string]*lead.Lead{}
	for _, in := range inputs {
		number := lead.NormalizeNumber(in.Number)
		out[number] = &lead.Lead{ID: "lead-" + number, Number: number}
	}
	return out, nil
}

type createFixture struct {
	campaigns *mockCampaignRepo
	entries   *capturingEntries
	leads     *numberedLeads
	screener  *fakeScreener
}

func newCreateFixture() *createFixture {
	return &createFixture{
		campaigns: newMockCampaignRepo(),
		entries:   &capturingEntries{mockEntryRepo: newMockEntryRepo()},
		leads:     &numberedLeads{qsLeadRepo: &qsLeadRepo{}},
		screener:  &fakeScreener{skip: map[string]campaign.SkipReason{}},
	}
}

func createPhonesAndTemplates() (*ownershipBusinessPhoneRepo, *mockTemplateRepo) {
	phones := newOwnershipBusinessPhoneRepo()
	phones.phones["bp-1"] = &businessphone.WhatsAppBusinessPhoneNumber{ID: "bp-1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1"}
	templates := newMockTemplateRepo()
	templates.templates["tmpl-1"] = &template.Template{ID: "tmpl-1", Name: "boas_vindas", Status: template.TemplateStatusApproved,
		Category: template.TemplateCategoryMarketing, WABAId: "waba-1"}
	return phones, templates
}

func (f *createFixture) useCase(screener campaignguard.Screener) wc.CreateCampaignUseCase {
	phones, templates := createPhonesAndTemplates()
	uc := NewCreateCampaignUseCase(f.campaigns, f.entries, f.leads, templates, phones, &ownershipWorkspacePhoneAccessRepo{hasAccess: true}, screener)
	uc.(*createCampaignUseCase).SetTemplateGrants(allowGrants{})
	uc.(*createCampaignUseCase).SetAutomation(automationThatAllows{})
	return uc
}

type automationThatAllows struct{}

func (automationThatAllows) Check(string, campaign.Automation, []campaign.EntryMetadata) error { return nil }

func selectionCampaign() *wc.Campaign {
	return &wc.Campaign{
		WorkspaceID: "ws-1", Name: "Campanha", TemplateID: "tmpl-1", BusinessPhoneID: "bp-1", Status: wc.CampaignStatusStopped,
		PhoneInputs: []wc.PhoneInput{
			{Number: "5584999990001"}, {Number: "5584999990002"}, {Number: "5584999990003"}, {Number: "5584999990004"},
		},
	}
}

func TestCreateFailsEveryIneligibleEntryWithItsCodeBeforeAnyDebit(t *testing.T) {
	f := newCreateFixture()
	f.screener.skip["lead-5584999990002"] = campaign.SkipBlocked
	f.screener.skip["lead-5584999990003"] = campaign.SkipOptedOut
	f.screener.skip["lead-5584999990004"] = campaign.SkipCooldown

	if _, err := f.useCase(f.screener).Execute(context.Background(), selectionCampaign()); err != nil {
		t.Fatalf("Execute() err = %v", err)
	}
	if len(f.screener.calls) != 1 || len(f.screener.calls[0].leadIDs) != 4 || f.screener.calls[0].senderID != "bp-1" || f.screener.calls[0].workspaceID != "ws-1" {
		t.Fatalf("screen calls = %+v, want one batch of the four leads from bp-1", f.screener.calls)
	}
	want := map[string]struct {
		status  wce.SendStatus
		code    int
		message string
	}{
		"lead-5584999990001": {wce.SendStatusPending, 0, ""},
		"lead-5584999990002": {wce.SendStatusFailed, 920001, "blocked"},
		"lead-5584999990003": {wce.SendStatusFailed, 920002, "opted_out"},
		"lead-5584999990004": {wce.SendStatusNotEligiblePossibleSpam, 920004, "cooldown"},
	}
	if len(f.entries.created) != 4 {
		t.Fatalf("created %d entries, want 4", len(f.entries.created))
	}
	for _, entry := range f.entries.created {
		expected := want[entry.LeadID]
		if entry.Status != expected.status || entry.ErrorCode != expected.code || entry.ErrorMessage != expected.message {
			t.Fatalf("entry of %s = (%s, %d, %q), want (%s, %d, %q)", entry.LeadID, entry.Status, entry.ErrorCode, entry.ErrorMessage, expected.status, expected.code, expected.message)
		}
	}
	if f.entries.updates != 0 {
		t.Fatalf("entries were updated one by one %d times, want them written already marked", f.entries.updates)
	}
}

func TestCreateWritesNothingWhenEligibilityCannotBeRead(t *testing.T) {
	f := newCreateFixture()
	f.screener.err = errors.New("db down")
	if _, err := f.useCase(f.screener).Execute(context.Background(), selectionCampaign()); err == nil {
		t.Fatal("an unreadable eligibility must refuse the campaign")
	}
	if len(f.campaigns.campaigns) != 0 || len(f.entries.created) != 0 {
		t.Fatalf("campaigns %d entries %d, want nothing written", len(f.campaigns.campaigns), len(f.entries.created))
	}
}

func TestCreateRefusesWithoutAnEligibilityCheck(t *testing.T) {
	f := newCreateFixture()
	if _, err := f.useCase(nil).Execute(context.Background(), selectionCampaign()); !errors.Is(err, campaignguard.ErrUnavailable) {
		t.Fatalf("Execute() err = %v, want ErrUnavailable", err)
	}
	if f.leads.resolved != 0 || len(f.campaigns.campaigns) != 0 || len(f.entries.created) != 0 {
		t.Fatal("nothing may be resolved or written without the eligibility check")
	}
}
