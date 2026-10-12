package studio_usecase

import (
	"context"
	"encoding/json"

	"vozko/domain/studio"
)

type PresetService struct {
	presets studio.PresetRepository
}

func NewPresetService(presets studio.PresetRepository) (*PresetService, error) {
	if presets == nil {
		return nil, ErrMissingDependency
	}
	return &PresetService{presets: presets}, nil
}

func (s *PresetService) List(ctx context.Context, workspaceID string) ([]studio.AnimationPreset, error) {
	builtins, err := studio.BuiltinPresets()
	if err != nil {
		return nil, err
	}
	saved, err := s.presets.ListPresets(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return append(builtins, saved...), nil
}

func (s *PresetService) Get(ctx context.Context, workspaceID, id string) (*studio.AnimationPreset, error) {
	if studio.IsBuiltinPresetID(id) {
		return studio.BuiltinPreset(id)
	}
	return s.presets.GetPreset(ctx, workspaceID, id)
}

func (s *PresetService) Create(ctx context.Context, workspaceID, userID, name, description string, animation json.RawMessage) (*studio.AnimationPreset, error) {
	p, err := studio.NewAnimationPreset(workspaceID, userID, name, description, animation)
	if err != nil {
		return nil, err
	}
	if err := s.presets.CreatePreset(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PresetService) Update(ctx context.Context, workspaceID, id string, change studio.PresetChange) (*studio.AnimationPreset, error) {
	if studio.IsBuiltinPresetID(id) {
		return nil, studio.ErrPresetReadOnly
	}
	p, err := s.presets.GetPreset(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if err := p.Apply(change); err != nil {
		return nil, err
	}
	if err := s.presets.SavePreset(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PresetService) Delete(ctx context.Context, workspaceID, id string) error {
	if studio.IsBuiltinPresetID(id) {
		return studio.ErrPresetReadOnly
	}
	return s.presets.DeletePreset(ctx, workspaceID, id)
}
