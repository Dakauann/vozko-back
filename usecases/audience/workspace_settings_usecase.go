package audience_usecase

import (
	"context"
	"fmt"

	ca "vozko/domain/audience"
)

// What the workspace decides about its own analysis: how much it may analyse in
// a rolling day, and how long a conversation must go quiet before it is judged.
//
// Both were constants or per-account fields before, which meant neither could be
// set by the person who actually runs the analysis. The ceiling lived on a
// channel account, so a workspace with no channel account could not set it. The
// debounce lived in the binary, so changing it was a deploy.

type workspaceSettingsUseCase struct {
	store ca.WorkspaceSettingsStore
}

func NewWorkspaceSettingsUseCase(store ca.WorkspaceSettingsStore) ca.WorkspaceSettingsUseCase {
	return &workspaceSettingsUseCase{store: store}
}

// Execute reports the settings as STORED, zeroes and all.
//
// Not the resolved values: a screen has to be able to tell "this workspace never
// set a debounce" from "this workspace deliberately set five minutes", because
// the first should render the default as a placeholder and the second as a
// value. Resolution is the reader's job, through ca.ClampDebounceMinutes and
// ca.ResolveDailyCap, which is also what the engine uses.
func (uc *workspaceSettingsUseCase) Execute(ctx context.Context, workspaceID string) (ca.WorkspaceSettings, error) {
	if workspaceID == "" {
		return ca.WorkspaceSettings{}, ca.ErrWorkspaceRequired
	}
	if uc.store == nil {
		return ca.WorkspaceSettings{}, nil
	}
	return uc.store.Get(ctx, workspaceID)
}

// Update applies the fields that were sent and leaves the rest alone.
//
// Values are REJECTED rather than clamped here. The read path clamps, because a
// row edited by hand must not be able to stop the sweep, but on the write path
// there is a person to tell: silently turning a typed 5000 into 1440 hands them
// a window they did not choose and no reason to doubt it.
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

	// Read, change what was sent, write back. The record is shared with the rest
	// of the workspace's configuration, so building a fresh one would blank
	// everything this use case does not know about.
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
