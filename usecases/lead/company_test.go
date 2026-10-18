package lead_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
)

const (
	acmeCompany    = "c-acme"
	closedCompany  = "c-closed"
	foreignCompany = "c-foreign"
)

type fakeCompanies map[string]leadcompany.Company

func (c fakeCompanies) GetCompany(_ context.Context, workspaceID, id string) (leadcompany.Company, error) {
	found, ok := c[id]
	if !ok || found.WorkspaceID != workspaceID {
		return leadcompany.Company{}, leadcompany.ErrNotFound
	}
	return found, nil
}

func (s *fakeStore) SaveMembership(_ context.Context, l *lead.Lead, expected int64, events []recordevent.Event) error {
	stored, ok := s.leads[l.ID]
	if !ok || stored.WorkspaceID != l.WorkspaceID {
		return lead.ErrLeadNotFound
	}
	if s.closing && l.CompanyID != "" {
		return leadcompany.ErrClosed
	}
	if stored.Version != expected {
		return shared.ErrVersionConflict
	}
	stored.CompanyID, stored.Version = l.CompanyID, expected+1
	l.Version = stored.Version
	s.joined = append(s.joined, savedCall{leadID: l.ID, expected: expected, events: events})
	return nil
}

func membershipPermissions() fakePermissions {
	return fakePermissions{"leads:read": true, "leads:update": true, "lead_companies:read": true}
}

func newCompanyFixture(t *testing.T, perms fakePermissions, leads ...*lead.Lead) *commandsFixture {
	t.Helper()
	f := newCommandsFixture(t, perms, leads...)
	f.companies[acmeCompany] = leadcompany.Company{ID: acmeCompany, WorkspaceID: cmdWorkspace, Name: "Acme", AcceptsLeads: true}
	f.companies[closedCompany] = leadcompany.Company{ID: closedCompany, WorkspaceID: cmdWorkspace, Name: "Closed"}
	f.companies[foreignCompany] = leadcompany.Company{ID: foreignCompany, WorkspaceID: "ws-2", Name: "Foreign", AcceptsLeads: true}
	return f
}

func TestSetCompanyPutsTheLeadInTheCompanyRecordsItAndNotifies(t *testing.T) {
	f := newCompanyFixture(t, membershipPermissions(), storedLead())
	got, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", acmeCompany)
	if err != nil {
		t.Fatalf("SetCompany: %v", err)
	}
	if got.CompanyID != acmeCompany || got.Version != 4 {
		t.Fatalf("lead = %+v", got)
	}
	if len(f.store.saves) != 0 {
		t.Fatal("the company is written only by the membership store, never by the general save")
	}
	if len(f.store.joined) != 1 || f.store.joined[0].expected != 3 {
		t.Fatalf("joined = %+v", f.store.joined)
	}
	wantEvent := recordevent.Event{Actor: cmdUser, Kind: lead.EventCompanyChanged, Changes: []recordevent.Change{{Field: lead.FieldCompany, After: acmeCompany}}}
	if events := f.store.joined[0].events; len(events) != 1 || !reflect.DeepEqual(events[0], wantEvent) {
		t.Fatalf("events = %+v, want %+v", events, wantEvent)
	}
	want := lead.Change{WorkspaceID: cmdWorkspace, LeadID: "l-1", Version: 4, Fields: []string{lead.FieldCompany}}
	if len(f.notifier.changes) != 1 || !reflect.DeepEqual(f.notifier.changes[0], want) {
		t.Fatalf("changes = %+v", f.notifier.changes)
	}
}

func TestSetCompanyRefusesAClosedCompanyWithoutWriting(t *testing.T) {
	f := newCompanyFixture(t, membershipPermissions(), storedLead())
	if _, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", closedCompany); !errors.Is(err, leadcompany.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if len(f.store.joined) != 0 || len(f.notifier.changes) != 0 {
		t.Fatal("a refused join must not write or notify")
	}
}

func TestACompanyThatClosesWhileTheLeadJoinsRefusesTheJoin(t *testing.T) {
	f := newCompanyFixture(t, membershipPermissions(), storedLead())
	f.store.closing = true
	if _, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", acmeCompany); !errors.Is(err, leadcompany.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if f.store.leads["l-1"].CompanyID != "" || len(f.notifier.changes) != 0 {
		t.Fatal("the store's refusal must leave the lead outside the company")
	}
}

func TestSetCompanyTreatsAnotherWorkspaceCompanyAsMissing(t *testing.T) {
	f := newCompanyFixture(t, membershipPermissions(), storedLead())
	if _, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", foreignCompany); !errors.Is(err, leadcompany.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestJoiningNeedsToSeeCompaniesButLeavingDoesNot(t *testing.T) {
	member := storedLead()
	member.CompanyID = acmeCompany
	f := newCompanyFixture(t, fakePermissions{"leads:read": true, "leads:update": true}, member)
	if _, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", closedCompany); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("join without lead_companies:read = %v, want ErrLeadForbidden", err)
	}
	left, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", " ")
	if err != nil || left.CompanyID != "" || f.store.leads["l-1"].CompanyID != "" {
		t.Fatalf("leave = %+v, err = %v", left, err)
	}
}

func TestSetCompanyNeedsLeadsUpdate(t *testing.T) {
	f := newCompanyFixture(t, fakePermissions{"leads:read": true, "lead_companies:read": true}, storedLead())
	if _, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", acmeCompany); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("err = %v, want ErrLeadForbidden", err)
	}
}

func TestStayingInTheSameCompanyWritesNothing(t *testing.T) {
	member := storedLead()
	member.CompanyID = closedCompany
	f := newCompanyFixture(t, membershipPermissions(), member)
	got, err := f.cmds.SetCompany(context.Background(), operator(), "l-1", closedCompany)
	if err != nil || got.CompanyID != closedCompany || len(f.store.joined) != 0 || len(f.notifier.changes) != 0 {
		t.Fatalf("lead = %+v, err = %v, joined = %d", got, err, len(f.store.joined))
	}
}
