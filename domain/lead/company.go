package lead

import (
	"context"

	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
)

const (
	FieldCompany        = "company"
	EventCompanyChanged = recordevent.Kind("company_changed")
)

type MembershipStore interface {
	SaveMembership(ctx context.Context, l *Lead, expectedVersion int64, events []recordevent.Event) error
}

func (l *Lead) JoinCompany(c leadcompany.Company) (bool, error) {
	if l.CompanyID == c.ID && c.WorkspaceID == l.WorkspaceID {
		return false, nil
	}
	if err := c.Admits(l.WorkspaceID); err != nil {
		return false, err
	}
	l.CompanyID = c.ID
	return true, nil
}

func (l *Lead) LeaveCompany() bool {
	if l.CompanyID == "" {
		return false
	}
	l.CompanyID = ""
	return true
}

func CompanyReleaseEvent(actorID, companyID string) recordevent.Event {
	return recordevent.Event{Actor: actorID, Kind: EventCompanyChanged, Changes: []recordevent.Change{{Field: FieldCompany, Before: companyID}}}
}
