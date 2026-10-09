package lead_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/opportunity"
)

type ownerNames struct {
	names map[string]string
	err   error
	asked [][]string
}

func (o *ownerNames) ResolveNames(ids ...string) (map[string]string, error) {
	o.asked = append(o.asked, ids)
	if o.err != nil {
		return nil, o.err
	}
	return o.names, nil
}

type dealCounter struct {
	count  int
	err    error
	scopes []opportunity.DealScope
}

func (d *dealCounter) CountDealsOfLead(_ context.Context, _, _ string, scope opportunity.DealScope) (int, error) {
	d.scopes = append(d.scopes, scope)
	return d.count, d.err
}

type memoryCounter struct {
	count int
	err   error
}

func (m memoryCounter) CountMemoriesOfLead(_ context.Context, _, _ string) (int, error) {
	return m.count, m.err
}

type numberHolders struct {
	holders []*lead.Lead
	err     error
	asked   [][]string
}

func (n *numberHolders) OtherHolders(_ context.Context, _, _ string, numbers []string) ([]*lead.Lead, error) {
	n.asked = append(n.asked, numbers)
	return n.holders, n.err
}

type summaryLeads map[string]*lead.Lead

func (s summaryLeads) LoadForDial(_ context.Context, workspaceID, leadID string) (*lead.Lead, error) {
	if l, ok := s[leadID]; ok && l.WorkspaceID == workspaceID {
		return l, nil
	}
	return nil, lead.ErrLeadNotFound
}

func readsLead() *lead.Lead {
	return &lead.Lead{ID: "l-1", WorkspaceID: "ws-1", Name: "Ana", Number: "5511987654321", Owner: "u-owner",
		Phones: []lead.ContactPhone{{ID: "p-1", Number: "551133334444", Label: lead.PhoneLandline}}}
}

func ownersHistory(t *testing.T, stored *lead.Lead, perms fakePermissions, owners *ownerNames) *History {
	t.Helper()
	h, err := NewHistory(HistoryDeps{
		Leads: historyLeads{stored.ID: stored}, Entries: historyEntries{}, Messages: historyMessages{}, Analyses: &historyAnalyses{},
		Access:  &accessResolver{byUser: map[string]map[string]bool{salesMember: {"e-sales": true}}},
		Windows: historyWindows{}, CampaignNames: historyCampaignNames{},
		Permissions: perms, Definitions: &fakeDefinitions{defs: leadDefinitions()},
		Relatives: &historyRelatives{}, EntryLeads: historyEntryLeads{"e-sales": stored.ID},
		Owners: owners,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTheDetailNamesTheOwner(t *testing.T) {
	owners := &ownerNames{names: map[string]string{"u-owner": "Marina Costa"}}
	detail, err := ownersHistory(t, readsLead(), fakePermissions{"leads:read": true}, owners).Detail(context.Background(), viewer(salesMember), "l-1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if detail.OwnerName != "Marina Costa" {
		t.Fatalf("owner name = %q", detail.OwnerName)
	}
}

func TestTheDetailAndTheCardFailWhenTheOwnerNameCannotBeRead(t *testing.T) {
	down := errors.New("users down")
	h := ownersHistory(t, readsLead(), fakePermissions{"leads:read": true}, &ownerNames{err: down})
	if _, err := h.Detail(context.Background(), viewer(salesMember), "l-1"); !errors.Is(err, down) {
		t.Fatalf("Detail err = %v, want the owner name failure", err)
	}
	if _, err := h.EntryLead(context.Background(), viewer(salesMember), "e-sales", ""); !errors.Is(err, down) {
		t.Fatalf("EntryLead err = %v, want the owner name failure", err)
	}
}

func TestTheInboxCardNamesTheOwnerAndCarriesTheOptOut(t *testing.T) {
	stored := readsLead()
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	stored.OptedOutAt, stored.OptOutSource = &at, lead.OptOutOperator
	owners := &ownerNames{names: map[string]string{"u-owner": "Marina Costa"}}
	card, err := ownersHistory(t, stored, fakePermissions{}, owners).EntryLead(context.Background(), conversation.Viewer{UserID: salesMember, WorkspaceID: "ws-1"}, "e-sales", "")
	if err != nil {
		t.Fatalf("EntryLead: %v", err)
	}
	if card.OwnerName != "Marina Costa" || card.OptedOutAt == nil || card.OptOutSource != lead.OptOutOperator {
		t.Fatalf("card = %+v", card)
	}
}

func TestAnUnownedLeadAsksForNoName(t *testing.T) {
	stored := readsLead()
	stored.Owner = ""
	owners := &ownerNames{err: errors.New("must not be asked")}
	h := ownersHistory(t, stored, fakePermissions{"leads:read": true}, owners)
	card, err := h.EntryLead(context.Background(), conversation.Viewer{UserID: salesMember, WorkspaceID: "ws-1"}, "e-sales", "")
	if err != nil || card.OwnerName != "" {
		t.Fatalf("card = %+v, %v", card, err)
	}
	if _, err := h.Detail(context.Background(), viewer(salesMember), "l-1"); err != nil {
		t.Fatalf("Detail: %v", err)
	}
	if len(owners.asked) != 0 {
		t.Fatalf("names asked for %v without an owner", owners.asked)
	}
}

func TestNewHistoryRefusesWithoutOwnerNames(t *testing.T) {
	_, err := NewHistory(HistoryDeps{
		Leads: historyLeads{}, Entries: historyEntries{}, Messages: historyMessages{}, Analyses: &historyAnalyses{}, Access: &accessResolver{},
		Windows: historyWindows{}, CampaignNames: historyCampaignNames{}, Permissions: fakePermissions{}, Definitions: &fakeDefinitions{},
		Relatives: &historyRelatives{}, EntryLeads: historyEntryLeads{},
	})
	if err == nil {
		t.Fatal("history without owner names must not be built")
	}
}

func summaryDeps() SummaryDeps {
	return SummaryDeps{
		Permissions: fakePermissions{"leads:read": true},
		Leads:       summaryLeads{"l-1": readsLead()},
		DealScopes:  dealScopes{byUser: map[string]opportunity.DealScope{}},
		Deals:       &dealCounter{},
		Memories:    memoryCounter{},
		Holders:     &numberHolders{},
	}
}

func summaries(t *testing.T, deps SummaryDeps) *DetailSummaries {
	t.Helper()
	s, err := NewDetailSummaries(deps)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheSummaryCountsDealsInTheViewersScopeAndMemories(t *testing.T) {
	scope := opportunity.DealScope{Restrict: true, AssigneeOverride: salesMember}
	deals := &dealCounter{count: 4}
	deps := summaryDeps()
	deps.DealScopes = dealScopes{byUser: map[string]opportunity.DealScope{salesMember: scope}}
	deps.Deals, deps.Memories = deals, memoryCounter{count: 7}

	summary, err := summaries(t, deps).Summary(context.Background(), viewer(salesMember), "l-1")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.MemoriesCount != 7 || summary.DealsCount == nil || *summary.DealsCount != 4 {
		t.Fatalf("summary = deals %v memories %d", summary.DealsCount, summary.MemoriesCount)
	}
	if len(deals.scopes) != 1 || deals.scopes[0].AssigneeOverride != salesMember || !deals.scopes[0].Restrict {
		t.Fatalf("the deal count must run inside the viewer's scope, got %+v", deals.scopes)
	}
}

func TestTheSummaryLeavesTheDealCountOutForAViewerWithoutDeals(t *testing.T) {
	deals := &dealCounter{count: 4}
	deps := summaryDeps()
	deps.Deals = deals
	summary, err := summaries(t, deps).Summary(context.Background(), viewer(salesMember), "l-1")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.DealsCount != nil || len(deals.scopes) != 0 {
		t.Fatalf("deals count = %v after %d counts, want no count and no query", summary.DealsCount, len(deals.scopes))
	}
}

func TestTheSummaryNamesTheOtherHoldersOfTheLeadsNumbers(t *testing.T) {
	holders := &numberHolders{holders: []*lead.Lead{
		{ID: "l-2", Name: "Joana", Number: "5511955554444", Phones: []lead.ContactPhone{{Number: "551133334444"}}},
	}}
	deps := summaryDeps()
	deps.Holders = holders
	summary, err := summaries(t, deps).Summary(context.Background(), viewer(salesMember), "l-1")
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(holders.asked) != 1 || len(holders.asked[0]) != 2 {
		t.Fatalf("asked = %v, want the identity and the contact phone in one read", holders.asked)
	}
	if len(summary.SharedNumbers) != 1 || summary.SharedNumbers[0].Number != "551133334444" || summary.SharedNumbers[0].Holders[0].LeadID != "l-2" {
		t.Fatalf("shared numbers = %+v", summary.SharedNumbers)
	}
}

func TestTheSummaryFailsWhenOneOfItsReadsFails(t *testing.T) {
	down := errors.New("database down")
	cases := map[string]func(*SummaryDeps){
		"deal scope": func(d *SummaryDeps) { d.DealScopes = dealScopes{err: down} },
		"deal count": func(d *SummaryDeps) {
			d.DealScopes = dealScopes{byUser: map[string]opportunity.DealScope{salesMember: {}}}
			d.Deals = &dealCounter{err: down}
		},
		"memories": func(d *SummaryDeps) { d.Memories = memoryCounter{err: down} },
		"holders":  func(d *SummaryDeps) { d.Holders = &numberHolders{err: down} },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			deps := summaryDeps()
			breakIt(&deps)
			if _, err := summaries(t, deps).Summary(context.Background(), viewer(salesMember), "l-1"); !errors.Is(err, down) {
				t.Fatalf("Summary err = %v, want the read failure", err)
			}
		})
	}
}

func TestTheSummaryRefusesWithoutLeadsReadOrAnUnknownLeadBeforeCounting(t *testing.T) {
	cases := map[string]struct {
		perms  fakePermissions
		leadID string
		want   error
	}{
		"no leads:read":          {fakePermissions{}, "l-1", lead.ErrLeadForbidden},
		"a lead of no workspace": {fakePermissions{"leads:read": true}, "l-404", lead.ErrLeadNotFound},
		"no lead id":             {fakePermissions{"leads:read": true}, " ", lead.ErrLeadRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			deals, holders := &dealCounter{}, &numberHolders{}
			deps := summaryDeps()
			deps.Permissions = tc.perms
			deps.DealScopes = dealScopes{byUser: map[string]opportunity.DealScope{salesMember: {}}}
			deps.Deals, deps.Holders = deals, holders
			if _, err := summaries(t, deps).Summary(context.Background(), viewer(salesMember), tc.leadID); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if len(deals.scopes) != 0 || len(holders.asked) != 0 {
				t.Fatal("nothing is counted for a refused summary")
			}
		})
	}
}

func TestNewDetailSummariesRefusesAMissingRead(t *testing.T) {
	for name, drop := range map[string]func(*SummaryDeps){
		"permissions": func(d *SummaryDeps) { d.Permissions = nil },
		"leads":       func(d *SummaryDeps) { d.Leads = nil },
		"deal scopes": func(d *SummaryDeps) { d.DealScopes = nil },
		"deals":       func(d *SummaryDeps) { d.Deals = nil },
		"memories":    func(d *SummaryDeps) { d.Memories = nil },
		"holders":     func(d *SummaryDeps) { d.Holders = nil },
	} {
		deps := summaryDeps()
		drop(&deps)
		if _, err := NewDetailSummaries(deps); err == nil {
			t.Errorf("summaries without %s must not be built", name)
		}
	}
}
