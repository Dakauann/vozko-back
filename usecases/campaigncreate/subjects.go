package campaigncreate

import (
	"context"
	"fmt"

	"vozko/domain/actor"
	"vozko/domain/campaign"
	"vozko/domain/lead"
)

type SubjectSource struct {
	Leads    lead.BindingLeads
	Contacts lead.ContactDetails
	Names    actor.Namer
}

func (s SubjectSource) Ready() bool {
	return s.Leads != nil && s.Contacts != nil && s.Names != nil
}

func (s SubjectSource) Subjects(ctx context.Context, workspaceID string, ids []string, bindings campaign.BindingPlan) (map[string]campaign.BindingSubject, error) {
	subjects := make(map[string]campaign.BindingSubject, len(ids))
	if !bindings.NeedsLead() || len(ids) == 0 {
		return subjects, nil
	}
	if !s.Ready() {
		return nil, campaign.ErrLeadTargetsUnavailable
	}
	found, err := s.Leads.BindingLeads(ctx, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("leads of the send: %w", err)
	}
	leads := make([]*lead.Lead, 0, len(found))
	for _, l := range found {
		if l != nil && l.WorkspaceID == workspaceID {
			leads = append(leads, l)
		}
	}
	return s.SubjectsOf(ctx, workspaceID, leads, bindings)
}

func (s SubjectSource) SubjectsOf(ctx context.Context, workspaceID string, leads []*lead.Lead, bindings campaign.BindingPlan) (map[string]campaign.BindingSubject, error) {
	subjects := make(map[string]campaign.BindingSubject, len(leads))
	if !bindings.NeedsLead() || len(leads) == 0 {
		return subjects, nil
	}
	if !s.Ready() {
		return nil, campaign.ErrLeadTargetsUnavailable
	}
	withID := make([]*lead.Lead, 0, len(leads))
	for _, l := range leads {
		if l != nil && l.ID != "" {
			withID = append(withID, l)
		}
	}
	if bindings.NeedsAddresses() && len(withID) > 0 {
		if err := s.Contacts.AttachContactDetails(ctx, workspaceID, withID); err != nil {
			return nil, fmt.Errorf("addresses of the send: %w", err)
		}
	}
	names := map[string]string{}
	if bindings.NeedsOwnerNames() {
		var owners []string
		for _, l := range withID {
			if l.Owner != "" {
				owners = append(owners, l.Owner)
			}
		}
		if len(owners) > 0 {
			names = s.Names.Names(owners...)
		}
	}
	for _, l := range withID {
		subjects[l.ID] = campaign.BindingSubject{Lead: l, OwnerName: names[l.Owner]}
	}
	return subjects, nil
}
