package carriermessaging_usecase

import (
	"context"
	"log"

	"vozko/domain/carriermessaging"
)

func (s *Service) ApplyStatus(ctx context.Context, report carriermessaging.StatusReport) error {
	next := report.Status
	if report.Unreachable {
		next = carriermessaging.StatusFailed
	}
	at := report.ReceivedAt
	if at.IsZero() {
		at = s.Now()
	}
	_, err := s.advance(ctx, report.Reference, next, report.ErrorCode, at)
	return err
}

func (s *Service) ExpireOverdue(ctx context.Context, limit int) (int, error) {
	now := s.Now()
	overdue, err := s.Repos.Messages.ListOverdue(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, m := range overdue {
		_, moved, err := s.Repos.Messages.Advance(ctx, m.Reference, carriermessaging.StatusExpired, "", now)
		if err != nil {
			log.Printf("[rcs] expire %s: %v", m.Reference, err)
			continue
		}
		if !moved {
			continue
		}
		expired++
		current, err := s.Repos.Messages.FindByReference(ctx, m.Reference)
		if err != nil {
			log.Printf("[rcs] expire %s reload: %v", m.Reference, err)
			continue
		}
		s.afterMove(ctx, *current)
	}
	return expired, nil
}
