package carriermessaging_usecase

import (
	"context"

	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
)

func (s *Service) GetSettings(ctx context.Context, workspaceID string) (carriermessaging.Settings, error) {
	return s.settingsFor(ctx, workspaceID)
}

func (s *Service) PutSettings(ctx context.Context, workspaceID string, settings carriermessaging.Settings) (carriermessaging.Settings, error) {
	settings.WorkspaceID = workspaceID
	if err := settings.Validate(); err != nil {
		return carriermessaging.Settings{}, err
	}
	if err := s.Repos.Settings.Put(ctx, settings); err != nil {
		return carriermessaging.Settings{}, err
	}
	return settings, nil
}

func (s *Service) ListSuppressions(ctx context.Context, workspaceID string, limit, offset int) ([]carriermessaging.Suppression, int, error) {
	return s.Repos.Suppressions.List(ctx, workspaceID, limit, offset)
}

func (s *Service) AddSuppression(ctx context.Context, workspaceID, phone string) error {
	normalized := shared.NormalizePhone(phone)
	if normalized == "" {
		return carriermessaging.ErrPhoneInvalid
	}
	_, err := s.Repos.Suppressions.Add(ctx, carriermessaging.Suppression{
		WorkspaceID: workspaceID, Phone: normalized, Source: carriermessaging.SuppressionOperator, CreatedAt: s.Now(),
	})
	return err
}

func (s *Service) RemoveSuppression(ctx context.Context, workspaceID, phone string) error {
	normalized := shared.NormalizePhone(phone)
	if normalized == "" {
		return carriermessaging.ErrPhoneInvalid
	}
	return s.Repos.Suppressions.Remove(ctx, workspaceID, normalized)
}
