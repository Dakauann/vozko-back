package lead_usecase

import (
	"context"
	"errors"
	"strings"

	"vozko/domain/lead"
	"vozko/domain/leadcompany"
	"vozko/domain/recordevent"
	"vozko/domain/workspace"
)

func (c *Commands) SetCompany(ctx context.Context, a Actor, id, companyID string) (*lead.Lead, error) {
	v, err := c.authorize(a, workspace.ActionUpdate)
	if err != nil {
		return nil, err
	}
	membership, err := c.membershipChange(ctx, a, strings.TrimSpace(companyID))
	if err != nil {
		return nil, err
	}
	sent := []string{lead.FieldCompany}
	changed, err := c.writer.retryingSave(ctx, a.WorkspaceID, id, func(current, next *lead.Lead) (recordEdit, error) {
		applied, err := membership(next)
		if err != nil || !applied {
			return recordEdit{}, err
		}
		return recordEdit{events: []recordevent.Event{lead.Changes(lead.EventCompanyChanged, a.UserID, current, next, nil)}, fields: sent}, nil
	}, c.deps.Memberships.SaveMembership)
	if errors.Is(err, errStillContended) {
		return nil, c.conflict(ctx, a, v, id, sent)
	}
	if err != nil {
		return nil, err
	}
	return changed.VisibleTo(v, sent), nil
}

func (c *Commands) membershipChange(ctx context.Context, a Actor, companyID string) (func(*lead.Lead) (bool, error), error) {
	if companyID == "" {
		return func(l *lead.Lead) (bool, error) { return l.LeaveCompany(), nil }, nil
	}
	if !workspace.HoldsCapability(c.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, leadcompany.CapabilityView) {
		return nil, lead.ErrLeadForbidden
	}
	company, err := c.deps.Companies.GetCompany(ctx, a.WorkspaceID, companyID)
	if err != nil {
		return nil, err
	}
	return func(l *lead.Lead) (bool, error) { return l.JoinCompany(company) }, nil
}
