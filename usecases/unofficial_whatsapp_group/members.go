package unofficial_whatsapp_group_usecase

import (
	"context"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func (s *Service) ListMembers(ctx context.Context, a Actor, groupID string, f uwg.MemberFilter) (*shared.PaginatedResult[*uwg.MemberRow], error) {
	n, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	f.GroupID, f.Scope = g.ID, n.scope
	return s.d.Members.List(ctx, a.WorkspaceID, f)
}

func (s *Service) GroupCampaigns(ctx context.Context, a Actor, groupID string) ([]uwg.GroupCampaign, error) {
	n, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	return s.d.Campaigns.ForGroup(ctx, a.WorkspaceID, g.JID, n.scope, shared.MaxPageSize)
}

func (s *Service) AddMembersToLeads(ctx context.Context, a Actor, groupID string, personKeys []string) (*LeadLinkResult, error) {
	_, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	members, err := s.d.Members.ByPersonKeys(ctx, g.ID, personKeys)
	if err != nil {
		return nil, err
	}
	return s.linkToLeads(ctx, a.WorkspaceID, members)
}

func (s *Service) AddPeopleToLeads(ctx context.Context, a Actor, personKeys []string) (*LeadLinkResult, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	members, err := s.d.Members.ByPeople(ctx, a.WorkspaceID, shared.DistinctTrimmed(personKeys), n.scope)
	if err != nil {
		return nil, err
	}
	return s.linkToLeads(ctx, a.WorkspaceID, members)
}

func (s *Service) linkToLeads(ctx context.Context, workspaceID string, members []uwg.Member) (*LeadLinkResult, error) {
	result := &LeadLinkResult{}
	for _, person := range uwg.MembershipsByPerson(members) {
		leadID := person.LeadID()
		switch {
		case leadID != "":
			result.AlreadyLinked++
		case person.Phone() == "":
			result.WithoutPhone++
			continue
		default:
			created, err := s.d.Leads.EnsureLeadForPhone(ctx, workspaceID, person.Phone(), person.Name())
			if err != nil {
				return result, err
			}
			leadID = created
			result.Linked++
		}
		for _, m := range person.Unlinked() {
			if err := s.d.Members.SetLead(ctx, m.GroupID, m.PersonKey, leadID); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
