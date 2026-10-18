package customerportfolio_usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cp "vozko/domain/customerportfolio"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
)

func (s *Service) PlaceConversation(ctx context.Context, a Actor, ref shared.EntryRef, holderID string) (cp.Holding, error) {
	if err := s.mayActOnConversation(a, ref); err != nil {
		return cp.Holding{}, err
	}
	return s.place(ctx, a, holderID, func() (cp.Subject, error) { return s.subjectOf(ctx, a.WorkspaceID, ref) })
}

func (s *Service) PlaceLead(ctx context.Context, a Actor, leadID, holderID string) (cp.Holding, error) {
	if err := s.mayReadLeads(a); err != nil {
		return cp.Holding{}, err
	}
	return s.place(ctx, a, holderID, func() (cp.Subject, error) { return cp.LeadSubject(leadID), nil })
}

func (s *Service) place(ctx context.Context, a Actor, holderID string, subjectOf func() (cp.Subject, error)) (cp.Holding, error) {
	holderID = strings.TrimSpace(holderID)
	if holderID == "" {
		holderID = a.UserID
	}
	caller, err := s.caller(a)
	if err != nil {
		return cp.Holding{}, err
	}
	if err := caller.MayPlaceFor(holderID); err != nil {
		return cp.Holding{}, err
	}
	if err := s.checkHolder(a, holderID); err != nil {
		return cp.Holding{}, err
	}
	subject, err := subjectOf()
	if err != nil {
		return cp.Holding{}, err
	}
	wanted, err := cp.New(a.WorkspaceID, holderID, a.UserID, subject, s.now())
	if err != nil {
		return cp.Holding{}, err
	}
	wanted.ID = s.deps.NewID()
	stored, inserted, err := s.deps.Entries.Insert(ctx, wanted)
	if err != nil {
		return cp.Holding{}, err
	}
	if !inserted {
		if stored, _, err = cp.Claim(&stored, wanted); err != nil {
			return cp.Holding{}, s.namedRefusal(err)
		}
	}
	return s.holding(subject, &stored)
}

func (s *Service) Move(ctx context.Context, a Actor, entryID, holderID string) (cp.Holding, error) {
	caller, err := s.caller(a)
	if err != nil {
		return cp.Holding{}, err
	}
	if err := caller.MayMove(); err != nil {
		return cp.Holding{}, err
	}
	entry, err := s.deps.Entries.Get(ctx, a.WorkspaceID, entryID)
	if err != nil {
		return cp.Holding{}, err
	}
	moved, changed, err := entry.MoveTo(holderID, s.now())
	if err != nil {
		return cp.Holding{}, err
	}
	if changed {
		if err := s.checkHolder(a, moved.HolderID); err != nil {
			return cp.Holding{}, err
		}
		if err := s.deps.Entries.SaveHolder(ctx, moved); err != nil {
			return cp.Holding{}, err
		}
	}
	return s.holding(moved.Subject, &moved)
}

func (s *Service) Remove(ctx context.Context, a Actor, entryID string) error {
	caller, err := s.caller(a)
	if err != nil {
		return err
	}
	entry, err := s.deps.Entries.Get(ctx, a.WorkspaceID, entryID)
	if err != nil {
		return err
	}
	if err := caller.MayRemove(entry); err != nil {
		return err
	}
	return s.deps.Entries.Delete(ctx, a.WorkspaceID, entry.ID)
}

func (s *Service) checkHolder(a Actor, holderID string) error {
	if holderID != a.UserID {
		visible, err := s.deps.Reach.CanView(a.UserID, holderID, a.WorkspaceID, a.IsAdmin)
		if err != nil {
			return fmt.Errorf("reach of %s: %w", holderID, err)
		}
		if !visible {
			return cp.ErrHolderOutOfReach
		}
	}
	if !ia.CanReceivePortfolio(s.deps.Receivers, holderID, a.WorkspaceID, "") {
		return cp.ErrHolderIneligible
	}
	return nil
}

func (s *Service) namedRefusal(err error) error {
	var held *cp.HeldByOther
	if !errors.As(err, &held) {
		return err
	}
	names, nameErr := s.deps.Names.ResolveNames(held.Entry.HolderID)
	if nameErr != nil {
		return fmt.Errorf("carteira holder name: %w", nameErr)
	}
	held.HolderName = names[held.Entry.HolderID]
	return held
}
