package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
)

type sweep struct{ s *Service }

func (s *Service) Sweep() interface {
	Execute(ctx context.Context) error
} {
	return sweep{s: s}
}

func (w sweep) Execute(ctx context.Context) error {
	return errors.Join(w.s.RunDueJoins(ctx), w.s.RunDueAnalyses(ctx))
}
