package customerportfolio_usecase

import (
	"context"
	"fmt"

	cp "vozko/domain/customerportfolio"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
)

func (s *Service) List(ctx context.Context, a Actor, q cp.ListQuery) (*shared.PaginatedResult[cp.Row], error) {
	caller, err := s.caller(a)
	if err != nil {
		return nil, err
	}
	if err := caller.MayList(q.HolderID); err != nil {
		return nil, err
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	page, err := s.deps.Entries.List(ctx, a.WorkspaceID, q)
	if err != nil {
		return nil, err
	}
	if err := s.describe(a.WorkspaceID, page.Items); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *Service) describe(workspaceID string, rows []cp.Row) error {
	people := map[string]bool{}
	for _, row := range rows {
		people[row.HolderID] = true
		if row.AddedBy != "" {
			people[row.AddedBy] = true
		}
	}
	if len(people) == 0 {
		return nil
	}
	ids := make([]string, 0, len(people))
	for id := range people {
		ids = append(ids, id)
	}
	names, err := s.deps.Names.ResolveNames(ids...)
	if err != nil {
		return fmt.Errorf("carteira names: %w", err)
	}
	receives := map[string]bool{}
	for i := range rows {
		holder := rows[i].HolderID
		if _, known := receives[holder]; !known {
			receives[holder] = ia.CanReceivePortfolio(s.deps.Receivers, holder, workspaceID, "")
		}
		rows[i].HolderName, rows[i].AddedByName, rows[i].HolderReceives = names[holder], names[rows[i].AddedBy], receives[holder]
	}
	return nil
}

func (s *Service) ForConversation(ctx context.Context, a Actor, ref shared.EntryRef) (cp.Holding, error) {
	if err := s.maySee(a); err != nil {
		return cp.Holding{}, err
	}
	if err := s.mayActOnConversation(a, ref); err != nil {
		return cp.Holding{}, err
	}
	subject, err := s.subjectOf(ctx, a.WorkspaceID, ref)
	if err != nil {
		return cp.Holding{}, err
	}
	return s.heldSubject(ctx, a, subject)
}

func (s *Service) ForLead(ctx context.Context, a Actor, leadID string) (cp.Holding, error) {
	if err := s.maySee(a); err != nil {
		return cp.Holding{}, err
	}
	if err := s.mayReadLeads(a); err != nil {
		return cp.Holding{}, err
	}
	return s.heldSubject(ctx, a, cp.LeadSubject(leadID))
}

func (s *Service) maySee(a Actor) error {
	caller, err := s.caller(a)
	if err != nil {
		return err
	}
	return caller.MaySee()
}

func (s *Service) heldSubject(ctx context.Context, a Actor, subject cp.Subject) (cp.Holding, error) {
	if err := subject.Validate(); err != nil {
		return cp.Holding{}, err
	}
	entry, err := s.deps.Entries.FindBySubject(ctx, a.WorkspaceID, subject)
	if err != nil {
		return cp.Holding{}, err
	}
	return s.holding(subject, entry)
}
