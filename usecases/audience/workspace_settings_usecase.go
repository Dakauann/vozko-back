package audience_usecase

import (
	"context"
	"fmt"

	ca "vozko/domain/audience"
)

type workspaceSettingsUseCase struct {
	store ca.WorkspaceSettingsStore
}

func NewWorkspaceSettingsUseCase(store ca.WorkspaceSettingsStore) ca.WorkspaceSettingsUseCase {
	return &workspaceSettingsUseCase{store: store}
}

func (uc *workspaceSettingsUseCase) Execute(ctx context.Context, workspaceID string) (ca.WorkspaceSettings, error) {
	if workspaceID == "" {
		return ca.WorkspaceSettings{}, ca.ErrWorkspaceRequired
	}
	if uc.store == nil {
		return ca.WorkspaceSettings{}, nil
	}
	return uc.store.Get(ctx, workspaceID)
}

func (uc *workspaceSettingsUseCase) Update(ctx context.Context, workspaceID string, in ca.UpdateWorkspaceSettingsInput) (ca.WorkspaceSettings, error) {
	if workspaceID == "" {
		return ca.WorkspaceSettings{}, ca.ErrWorkspaceRequired
	}
	if uc.store == nil {
		return ca.WorkspaceSettings{}, fmt.Errorf("%w: analysis settings are not writable here", ca.ErrInvalidFilter)
	}
	if in.DailyCap == nil && in.DebounceMinutes == nil {
		return ca.WorkspaceSettings{}, fmt.Errorf("%w: nothing to change", ca.ErrInvalidFilter)
	}
	if in.DailyCap != nil && *in.DailyCap <= 0 {
		return ca.WorkspaceSettings{}, fmt.Errorf("%w: the analysis limit must be positive", ca.ErrInvalidFilter)
	}
	if in.DebounceMinutes != nil && !ca.ValidDebounceMinutes(*in.DebounceMinutes) {
		return ca.WorkspaceSettings{}, fmt.Errorf("%w: the quiet period must be between %d and %d minutes",
			ca.ErrInvalidFilter, ca.MinDebounceMinutes, ca.MaxDebounceMinutes)
	}

	current, err := uc.store.Get(ctx, workspaceID)
	if err != nil {
		return ca.WorkspaceSettings{}, err
	}
	if in.DailyCap != nil {
		current.DailyCap = *in.DailyCap
	}
	if in.DebounceMinutes != nil {
		current.DebounceMinutes = *in.DebounceMinutes
	}
	if err := uc.store.Save(ctx, workspaceID, current); err != nil {
		return ca.WorkspaceSettings{}, err
	}
	return current, nil
}
