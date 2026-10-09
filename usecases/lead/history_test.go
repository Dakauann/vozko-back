package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/lead_message_window"
	"vozko/domain/shared"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type historyLeads map[string]*lead.Lead

func (h historyLeads) FindByID(workspaceID, id string) (*lead.Lead, error) {
	if l, ok := h[id]; ok && l.WorkspaceID == workspaceID {
		return l, nil
	}
	return nil, lead.ErrLeadNotFound
}

type historyEntries struct {
	byLead map[string][]wce.WhatsAppCampaignEntry
}

func (e historyEntries) ListByLeadID(leadID string) ([]wce.WhatsAppCampaignEntry, error) {
	return e.byLead[leadID], nil
}

func (e historyEntries) FindByID(id string) (*wce.WhatsAppCampaignEntry, error) {
	for _, entries := range e.byLead {
		for i := range entries {
			if entries[i].ID == id {
				return &entries[i], nil
			}
		}
	}
	return nil, wce.ErrEntryNotFound
}

type historyMessages struct {
	byEntry map[string][]*conversation.Message
}

func (m historyMessages) ListByEntry(entryID string, _ shared.EntryType) ([]*conversation.Message, error) {
	return m.byEntry[entryID], nil
}

type historyAnalyses struct{ asked [][]string }

func (a *historyAnalyses) LatestByEntries(_ context.Context, _ string, _ ca.Source, entryIDs []string) (map[string]*ca.Analysis, error) {
	a.asked = append(a.asked, entryIDs)
	out := map[string]*ca.Analysis{}
	for _, id := range entryIDs {
		out[id] = &ca.Analysis{ID: "a-" + id}
	}
	return out, nil
}

type deptAccess struct {
	allowed map[string]bool
	checks  *int
	err     error
}

func (d deptAccess) VisibleEntries(refs []shared.EntryRef) (map[shared.EntryRef]bool, error) {
	*d.checks++
	if d.err != nil {
		return nil, d.err
	}
	out := make(map[shared.EntryRef]bool, len(refs))
	for _, ref := range refs {
		out[ref] = d.allowed[ref.EntryID]
	}
	return out, nil
}

type accessResolver struct {
	byUser map[string]map[string]bool
	calls  int
	checks int
	err    error
}

func (r *accessResolver) EntryVisibilityFor(userID, _ string, _ bool) conversation.EntryVisibility {
	r.calls++
	return deptAccess{allowed: r.byUser[userID], checks: &r.checks, err: r.err}
}

type historyWindows struct {
	windows []*lead_message_window.LeadMessageWindow
	err     error
}

func (w historyWindows) FindAllByLead(string) ([]*lead_message_window.LeadMessageWindow, error) {
	return w.windows, w.err
}

type historyCampaignNames map[string]string

func (n historyCampaignNames) ResolveCampaignNames(ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := n[id]; ok {
			out["whatsapp:"+id] = name
		}
	}
	return out
}

var historyNow = time.Now().UTC()

const (
	salesMember   = "u-sales"
	supportMember = "u-support"
)

func newHistoryFixture(t *testing.T) (*History, *historyAnalyses, *accessResolver) {
	t.Helper()
	entries := []wce.WhatsAppCampaignEntry{
		{ID: "e-sales", CampaignID: "c-1", LeadID: "l-1", UpdatedAt: historyNow.Add(-3 * time.Hour)},
		{ID: "e-support", CampaignID: "c-1", LeadID: "l-1", UpdatedAt: historyNow.Add(-2 * time.Hour)},
		{ID: "e-other-campaign", CampaignID: "c-2", LeadID: "l-1", UpdatedAt: historyNow.Add(-30 * time.Minute)},
	}
	analyses := &historyAnalyses{}
	access := &accessResolver{byUser: map[string]map[string]bool{
		salesMember:   {"e-sales": true, "e-other-campaign": true},
		supportMember: {"e-support": true},
	}}
	h, err := NewHistory(HistoryDeps{
		Leads:   historyLeads{"l-1": {ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana"}},
		Entries: historyEntries{byLead: map[string][]wce.WhatsAppCampaignEntry{"l-1": entries}},
		Messages: historyMessages{
			byEntry: map[string][]*conversation.Message{"e-support": {{ID: "m-2", EntryID: "e-support", EntryType: shared.EntryTypeWhatsApp}}},
		},
		Analyses:      analyses,
		Access:        access,
		Windows:       historyWindows{windows: []*lead_message_window.LeadMessageWindow{{LeadID: "l-1", LastMessageAt: historyNow.Add(-time.Hour)}}},
		CampaignNames: historyCampaignNames{"c-1": "Boas-vindas", "c-2": "Retorno"},
		Permissions:   fakePermissions{"leads:read": true},
		Definitions:   &fakeDefinitions{defs: leadDefinitions()},
		Relatives:     &historyRelatives{},
		EntryLeads:    historyEntryLeads{"e-sales": "l-1", "e-support": "l-1"},
		Owners:        &ownerNames{},
	})
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}
	return h, analyses, access
}

func viewer(userID string) conversation.Viewer {
	return conversation.Viewer{UserID: userID, WorkspaceID: "ws-1"}
}

func TestNewHistoryRefusesAMissingDependency(t *testing.T) {
	full := HistoryDeps{
		Leads: historyLeads{}, Entries: historyEntries{}, Messages: historyMessages{}, Analyses: &historyAnalyses{}, Access: &accessResolver{},
		Windows: historyWindows{}, CampaignNames: historyCampaignNames{}, Permissions: fakePermissions{}, Definitions: &fakeDefinitions{},
		Relatives: &historyRelatives{}, EntryLeads: historyEntryLeads{}, Owners: &ownerNames{},
	}
	for name, drop := range map[string]func(*HistoryDeps){
		"relatives":      func(d *HistoryDeps) { d.Relatives = nil },
		"entry leads":    func(d *HistoryDeps) { d.EntryLeads = nil },
		"permissions":    func(d *HistoryDeps) { d.Permissions = nil },
		"definitions":    func(d *HistoryDeps) { d.Definitions = nil },
		"leads":          func(d *HistoryDeps) { d.Leads = nil },
		"entries":        func(d *HistoryDeps) { d.Entries = nil },
		"messages":       func(d *HistoryDeps) { d.Messages = nil },
		"analyses":       func(d *HistoryDeps) { d.Analyses = nil },
		"access":         func(d *HistoryDeps) { d.Access = nil },
		"windows":        func(d *HistoryDeps) { d.Windows = nil },
		"campaign names": func(d *HistoryDeps) { d.CampaignNames = nil },
	} {
		deps := full
		drop(&deps)
		if _, err := NewHistory(deps); err == nil {
			t.Errorf("history without %s must not be built", name)
		}
	}
}

func TestEntriesAreOnlyTheConversationsTheViewerMayOpen(t *testing.T) {
	h, _, access := newHistoryFixture(t)
	_, entries, err := h.Entries(viewer(supportMember), "l-1")
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "e-support" {
		t.Fatalf("a support member sees only the support conversation, got %+v", entries)
	}
	if access.calls != 1 || access.checks != 1 {
		t.Fatalf("the viewer scope is resolved and checked once per call, got %d resolutions and %d checks", access.calls, access.checks)
	}
}

func TestEntriesInACampaignAreFilteredByCampaignAndAccess(t *testing.T) {
	h, _, _ := newHistoryFixture(t)
	entries, err := h.EntriesInCampaign(viewer(salesMember), "l-1", "c-1", "")
	if err != nil {
		t.Fatalf("EntriesInCampaign: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "e-sales" {
		t.Fatalf("got %+v", entries)
	}
}

func TestAnalysesAreReadOnlyForVisibleEntries(t *testing.T) {
	h, analyses, _ := newHistoryFixture(t)
	_, got, err := h.Analyses(context.Background(), viewer(supportMember), "l-1", "c-1", "")
	if err != nil {
		t.Fatalf("Analyses: %v", err)
	}
	if len(got) != 1 || got[0].ID != "a-e-support" {
		t.Fatalf("got %+v", got)
	}
	if len(analyses.asked) != 1 || len(analyses.asked[0]) != 1 || analyses.asked[0][0] != "e-support" {
		t.Fatalf("analyses of hidden entries must not even be read: %v", analyses.asked)
	}
}

func TestAMemberWithoutAccessToAnyEntryGetsNothingAndNoAnalysisRead(t *testing.T) {
	h, analyses, _ := newHistoryFixture(t)
	_, got, err := h.Analyses(context.Background(), viewer("u-nobody"), "l-1", "c-1", "")
	if err != nil || len(got) != 0 || len(analyses.asked) != 0 {
		t.Fatalf("got %+v, %v, asked %v", got, err, analyses.asked)
	}
}

func TestEntryConversationIsHiddenOutsideTheViewerScope(t *testing.T) {
	h, _, _ := newHistoryFixture(t)
	if _, err := h.EntryConversation(viewer(salesMember), "e-support", shared.EntryTypeWhatsApp); !errors.Is(err, ErrConversationNotVisible) {
		t.Fatalf("err = %v, want ErrConversationNotVisible", err)
	}
	got, err := h.EntryConversation(viewer(supportMember), "e-support", shared.EntryTypeWhatsApp)
	if err != nil || len(got.Messages) != 1 || got.LeadID != "l-1" || got.CampaignID != "c-1" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestALeadOfAnotherWorkspaceIsNotFoundOnEveryRead(t *testing.T) {
	h, _, _ := newHistoryFixture(t)
	foreign := conversation.Viewer{UserID: salesMember, WorkspaceID: "ws-2"}
	if _, _, err := h.Entries(foreign, "l-1"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("Entries err = %v", err)
	}
	if _, err := h.EntriesInCampaign(foreign, "l-1", "c-1", ""); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("EntriesInCampaign err = %v", err)
	}
	if _, _, err := h.Analyses(context.Background(), foreign, "l-1", "c-1", ""); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("Analyses err = %v", err)
	}
}

func TestAnUnresolvedViewerScopeShowsNothing(t *testing.T) {
	h, _, _ := newHistoryFixture(t)
	_, entries, err := h.Entries(conversation.Viewer{WorkspaceID: "ws-1"}, "l-1")
	if err != nil || len(entries) != 0 {
		t.Fatalf("a viewer without an id sees nothing, got %+v, %v", entries, err)
	}
}

func TestAnotherChannelHasNoCampaignEntriesOrAnalysesButTheLeadIsStillChecked(t *testing.T) {
	h, analyses, _ := newHistoryFixture(t)
	entries, err := h.EntriesInCampaign(viewer(salesMember), "l-1", "c-1", shared.EntryTypeTelegram)
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	if _, got, err := h.Analyses(context.Background(), viewer(salesMember), "l-1", "c-1", shared.EntryTypeTelegram); err != nil || len(got) != 0 || len(analyses.asked) != 0 {
		t.Fatalf("analyses = %+v, %v, asked %v", got, err, analyses.asked)
	}
	foreign := conversation.Viewer{UserID: salesMember, WorkspaceID: "ws-2"}
	if _, err := h.EntriesInCampaign(foreign, "l-1", "c-1", shared.EntryTypeTelegram); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("a lead of another workspace is not found on any channel, got %v", err)
	}
}

func TestDetailGroupsTheVisibleConversationsByCampaignWithTheWindowSummary(t *testing.T) {
	h, _, access := newHistoryFixture(t)
	detail, err := h.Detail(context.Background(), viewer(salesMember), "l-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.Lead.ID != "l-1" || len(detail.Campaigns) != 2 || access.checks != 1 {
		t.Fatalf("detail = %+v, checks %d", detail, access.checks)
	}
	first, second := detail.Campaigns[0], detail.Campaigns[1]
	if first.CampaignID != "c-1" || first.CampaignName != "Boas-vindas" || len(first.Entries) != 1 || first.Entries[0].ID != "e-sales" {
		t.Fatalf("first campaign = %+v", first)
	}
	if second.CampaignID != "c-2" || second.CampaignName != "Retorno" || !second.LastActivityAt.Equal(historyNow.Add(-30*time.Minute)) {
		t.Fatalf("second campaign = %+v", second)
	}
	summary := detail.Summary
	if summary.WhatsAppCampaigns != 2 || summary.TotalCampaigns != 2 || !summary.WhatsAppWindowOpen || summary.WindowExpiresAt == nil {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.LastActivityAt == nil || !summary.LastActivityAt.Equal(historyNow.Add(-30*time.Minute)) {
		t.Fatalf("the last activity is the newest of the window and the visible entries, got %v", summary.LastActivityAt)
	}
}

func TestDetailFailsWhenTheWindowCannotBeRead(t *testing.T) {
	h, err := NewHistory(HistoryDeps{
		Leads:         historyLeads{"l-1": {ID: "l-1", WorkspaceID: "ws-1", Name: "Ana"}},
		Entries:       historyEntries{},
		Messages:      historyMessages{},
		Analyses:      &historyAnalyses{},
		Access:        &accessResolver{},
		Windows:       historyWindows{err: errors.New("database down")},
		CampaignNames: historyCampaignNames{},
		Permissions:   fakePermissions{"leads:read": true},
		Definitions:   &fakeDefinitions{},
		Relatives:     &historyRelatives{},
		EntryLeads:    historyEntryLeads{},
		Owners:        &ownerNames{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Detail(context.Background(), viewer(salesMember), "l-1"); err == nil {
		t.Fatal("an unreadable window must fail the detail, not report a closed window")
	}
}

func TestAnUnreadableScopeFailsTheReadInsteadOfHidingOrShowing(t *testing.T) {
	h, _, access := newHistoryFixture(t)
	access.err = errors.New("database down")
	if _, _, err := h.Entries(viewer(salesMember), "l-1"); err == nil {
		t.Fatal("Entries must fail when the scope cannot be checked")
	}
	if _, err := h.EntryConversation(viewer(salesMember), "e-sales", shared.EntryTypeWhatsApp); err == nil || errors.Is(err, ErrConversationNotVisible) {
		t.Fatalf("EntryConversation must fail when the scope cannot be checked, got %v", err)
	}
}

func (h historyLeads) Load(_ context.Context, workspaceID, id string) (*lead.Lead, error) {
	return h.FindByID(workspaceID, id)
}

type historyRelatives struct {
	page  lead.RelativesPage
	err   error
	asked []lead.RelativesQuery
}

func (r *historyRelatives) ListRelatives(_ context.Context, _ string, q lead.RelativesQuery) (lead.RelativesPage, error) {
	r.asked = append(r.asked, q)
	return r.page, r.err
}

type historyEntryLeads map[string]string

func (e historyEntryLeads) LeadOfEntry(_ context.Context, _ string, ref shared.EntryRef) (string, error) {
	if id, ok := e[ref.EntryID]; ok {
		return id, nil
	}
	return "", lead.ErrLeadNotFound
}

func familyHistory(t *testing.T, leads historyLeads, perms fakePermissions, relatives *historyRelatives) *History {
	t.Helper()
	h, err := NewHistory(HistoryDeps{
		Leads: leads, Entries: historyEntries{}, Messages: historyMessages{}, Analyses: &historyAnalyses{},
		Access:  &accessResolver{byUser: map[string]map[string]bool{salesMember: {"e-sales": true, "e-orphan": true}}},
		Windows: historyWindows{}, CampaignNames: historyCampaignNames{},
		Permissions: perms, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Relatives: relatives, EntryLeads: historyEntryLeads{"e-sales": "l-1", "e-hidden": "l-1"},
		Owners: &ownerNames{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTheDetailCountsTheFamilyAndLeavesTheListToItsOwnPage(t *testing.T) {
	leads := historyLeads{"l-1": {ID: "l-1", WorkspaceID: "ws-1", Name: "Ana", RelativesCount: 3, ReferredCount: 1200,
		Phones: []lead.ContactPhone{{ID: "p-1", Number: "551133334444", Label: lead.PhoneLandline}}}}
	relatives := &historyRelatives{}
	detail, err := familyHistory(t, leads, fakePermissions{"leads:read": true}, relatives).Detail(context.Background(), viewer(salesMember), "l-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(detail.Lead.Phones) != 1 || detail.Lead.RelativesCount != 3 || detail.Lead.ReferredCount != 1200 {
		t.Fatalf("the detail carries the aggregate and the counts, got %+v", detail.Lead)
	}
	if len(relatives.asked) != 0 {
		t.Fatal("opening the detail must not read a single relation")
	}
}

func TestTheRelativesPageNeedsLeadsReadAndAnExistingLead(t *testing.T) {
	leads := historyLeads{"l-1": {ID: "l-1", WorkspaceID: "ws-1", Name: "Ana"}}
	page := lead.RelativesPage{Relatives: []lead.Relative{{Relation: lead.Relation{ID: "r-1", LeadID: "l-1", OtherLeadID: "l-2", Kind: lead.KindReferred}, Lead: &lead.Lead{ID: "l-2", Name: "Pedro"}}}, Next: "cursor"}
	relatives := &historyRelatives{page: page}
	h := familyHistory(t, leads, fakePermissions{"leads:read": true}, relatives)

	q := lead.RelativesQuery{LeadID: "l-1", Dimension: lead.DimensionReferral, Limit: 10}
	got, err := h.Relatives(context.Background(), viewer(salesMember), q)
	if err != nil || got.Next != "cursor" || len(got.Relatives) != 1 {
		t.Fatalf("Relatives = %+v, %v", got, err)
	}
	if len(relatives.asked) != 1 || relatives.asked[0] != q {
		t.Fatalf("asked = %+v", relatives.asked)
	}
	if _, err := h.Relatives(context.Background(), viewer(salesMember), lead.RelativesQuery{LeadID: "ghost"}); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("an unknown lead = %v", err)
	}
	blind := familyHistory(t, leads, fakePermissions{}, &historyRelatives{})
	if _, err := blind.Relatives(context.Background(), viewer(salesMember), q); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:read = %v", err)
	}
	failing := familyHistory(t, leads, fakePermissions{"leads:read": true}, &historyRelatives{err: errors.New("database down")})
	if _, err := failing.Relatives(context.Background(), viewer(salesMember), q); err == nil {
		t.Fatal("an unreadable family must fail, not show an empty page")
	}
}

func TestTheEntryLeadCardFollowsTheConversationAndTheFieldRules(t *testing.T) {
	leads := historyLeads{"l-1": {ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321", Name: "Ana", Owner: salesMember, RelativesCount: 2, Version: 7,
		CustomFields: map[string]any{"cor": "azul", "classificacao": "positivo"},
		Addresses:    []lead.Address{{ID: "a-1", Label: lead.AddressHome, Primary: true, Postal: familyHome}}}}
	h := familyHistory(t, leads, fakePermissions{"conversations:read": true}, &historyRelatives{})

	card, err := h.EntryLead(context.Background(), viewer(salesMember), "e-sales", shared.EntryTypeWhatsApp)
	if err != nil {
		t.Fatalf("EntryLead: %v", err)
	}
	if card.LeadID != "l-1" || card.Version != 7 || card.Owner != salesMember || card.RelativesCount != 2 {
		t.Fatalf("card = %+v", card)
	}
	if card.Area == nil || card.Area.District != "Bela Vista" || card.Area.City != "São Paulo" {
		t.Fatalf("area = %+v", card.Area)
	}
	if _, sensitive := card.CustomFields["classificacao"]; sensitive || card.CustomFields["cor"] != "azul" {
		t.Fatalf("custom fields = %v", card.CustomFields)
	}
	if _, err := h.EntryLead(context.Background(), viewer(salesMember), "e-hidden", shared.EntryTypeWhatsApp); !errors.Is(err, ErrConversationNotVisible) {
		t.Fatalf("a conversation the member cannot open = %v", err)
	}
	if _, err := h.EntryLead(context.Background(), viewer(salesMember), "e-orphan", shared.EntryTypeWhatsApp); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("a conversation without a lead = %v", err)
	}
}
