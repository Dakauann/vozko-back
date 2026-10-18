package lead

import (
	"errors"
	"reflect"
	"testing"

	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
)

const (
	companyWorkspace = "ws-1"
	acmeID           = "c-acme"
	globexID         = "c-globex"
)

func company(id string, open bool) leadcompany.Company {
	return leadcompany.Company{ID: id, WorkspaceID: companyWorkspace, Name: id, AcceptsLeads: open}
}

func TestJoinCompanyPutsTheLeadInAnOpenCompanyOfItsWorkspace(t *testing.T) {
	l := &Lead{ID: "l-1", WorkspaceID: companyWorkspace}
	changed, err := l.JoinCompany(company(acmeID, true))
	if err != nil || !changed || l.CompanyID != acmeID {
		t.Fatalf("changed = %v, company = %q, err = %v", changed, l.CompanyID, err)
	}
}

func TestJoinCompanyRefusesAClosedCompanyAndKeepsTheCurrentOne(t *testing.T) {
	l := &Lead{ID: "l-1", WorkspaceID: companyWorkspace, CompanyID: acmeID}
	changed, err := l.JoinCompany(company(globexID, false))
	if !errors.Is(err, leadcompany.ErrClosed) || changed || l.CompanyID != acmeID {
		t.Fatalf("changed = %v, company = %q, err = %v", changed, l.CompanyID, err)
	}
}

func TestJoinCompanyRefusesACompanyOfAnotherWorkspaceAsNotFound(t *testing.T) {
	other := company(acmeID, true)
	other.WorkspaceID = "ws-2"
	l := &Lead{ID: "l-1", WorkspaceID: companyWorkspace}
	if _, err := l.JoinCompany(other); !errors.Is(err, leadcompany.ErrNotFound) || l.CompanyID != "" {
		t.Fatalf("company = %q, err = %v", l.CompanyID, err)
	}
}

func TestStayingInTheSameCompanyIsANoOpEvenAfterItClosed(t *testing.T) {
	l := &Lead{ID: "l-1", WorkspaceID: companyWorkspace, CompanyID: acmeID}
	changed, err := l.JoinCompany(company(acmeID, false))
	if err != nil || changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
}

func TestLeaveCompanyIsAlwaysAllowedAndReportsWhetherItChanged(t *testing.T) {
	l := &Lead{ID: "l-1", WorkspaceID: companyWorkspace, CompanyID: acmeID}
	if !l.LeaveCompany() || l.CompanyID != "" {
		t.Fatalf("company = %q after leaving", l.CompanyID)
	}
	if l.LeaveCompany() {
		t.Fatal("leaving twice changes nothing")
	}
}

func TestTheCompanyIsPartOfTheRecordedFields(t *testing.T) {
	before := &Lead{ID: "l-1", WorkspaceID: companyWorkspace, Name: "Ana"}
	after := *before
	after.CompanyID = acmeID
	event := Changes(EventCompanyChanged, "u-1", before, &after, nil)
	want := []recordevent.Change{{Field: FieldCompany, After: acmeID}}
	if !reflect.DeepEqual(event.Changes, want) {
		t.Fatalf("changes = %+v, want %+v", event.Changes, want)
	}
}

func TestCompanyReleaseEventRecordsTheCompanyTheLeadLeft(t *testing.T) {
	event := CompanyReleaseEvent("u-1", acmeID)
	want := recordevent.Event{Actor: "u-1", Kind: EventCompanyChanged, Changes: []recordevent.Change{{Field: FieldCompany, Before: acmeID}}}
	if !reflect.DeepEqual(event, want) {
		t.Fatalf("event = %+v, want %+v", event, want)
	}
}

func TestCompanyRefusalsCarryTheirCodesAndAreNotMalformedInput(t *testing.T) {
	for err, want := range map[error]string{leadcompany.ErrNotFound: "lead_company_not_found", leadcompany.ErrClosed: "lead_company_closed"} {
		if got := ErrorCode(err); got != want {
			t.Errorf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
		if IsInputRefusal(err) {
			t.Errorf("%v must not be an input refusal", err)
		}
	}
}

func TestTheCompanyIsSentToAViewerWhoOnlyChangedIt(t *testing.T) {
	l := &Lead{ID: "l-1", WorkspaceID: companyWorkspace, Name: "Ana", CompanyID: acmeID, Version: 4}
	visible := l.VisibleTo(Viewer{}, []string{FieldCompany})
	if visible.CompanyID != acmeID || visible.Name != "" {
		t.Fatalf("visible = %+v", visible)
	}
}
