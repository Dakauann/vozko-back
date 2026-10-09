package workspace_config_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/geocoding"
	wsc "vozko/domain/workspace_config"
)

var errGeocodingSettingsIncomplete = errors.New("geocoding settings: a required dependency is missing")

type GeocodingSettingsDeps struct {
	Store      geocoding.SettingsStore
	Usage      geocoding.UsageStore
	Pauses     geocoding.PauseReader
	Workspaces WorkspaceDirectory
	Names      ActorNames
	Providers  []geocoding.Provider
	Now        func() time.Time
}

type GeocodingSettingsView struct {
	Settings              geocoding.Settings
	ProviderChangedByName string
	CeilingChangedByName  string
	Usage                 geocoding.Usage
	Slot                  *geocoding.Slot
	Providers             []geocoding.Provider
	CanChangeProvider     bool
	CanChangeCeiling      bool
	Pause                 *geocoding.ProviderPause
	PauseUnreadable       bool
}

type GeocodingSettingsUseCase struct {
	deps GeocodingSettingsDeps
}

func NewGeocodingSettingsUseCase(deps GeocodingSettingsDeps) (*GeocodingSettingsUseCase, error) {
	switch {
	case deps.Store == nil:
		return nil, fmt.Errorf("%w: store", errGeocodingSettingsIncomplete)
	case deps.Usage == nil:
		return nil, fmt.Errorf("%w: usage", errGeocodingSettingsIncomplete)
	case deps.Pauses == nil:
		return nil, fmt.Errorf("%w: pauses", errGeocodingSettingsIncomplete)
	case deps.Workspaces == nil:
		return nil, fmt.Errorf("%w: workspaces", errGeocodingSettingsIncomplete)
	case deps.Names == nil:
		return nil, fmt.Errorf("%w: names", errGeocodingSettingsIncomplete)
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &GeocodingSettingsUseCase{deps: deps}, nil
}

func (uc *GeocodingSettingsUseCase) configured(p geocoding.Provider) bool {
	for _, available := range uc.deps.Providers {
		if available == p {
			return true
		}
	}
	return false
}

func (uc *GeocodingSettingsUseCase) Get(ctx context.Context, workspaceID string, caller wsc.Caller) (GeocodingSettingsView, error) {
	editor, err := uc.editor(workspaceID, caller)
	if err != nil {
		return GeocodingSettingsView{}, err
	}
	settings, err := uc.deps.Store.Settings(ctx, workspaceID)
	if err != nil {
		return GeocodingSettingsView{}, err
	}
	return uc.view(ctx, workspaceID, settings, editor)
}

func (uc *GeocodingSettingsUseCase) Change(ctx context.Context, workspaceID string, caller wsc.Caller, change geocoding.Change) (GeocodingSettingsView, error) {
	editor, err := uc.editor(workspaceID, caller)
	if err != nil {
		return GeocodingSettingsView{}, err
	}
	settings, err := uc.deps.Store.ChangeSettings(ctx, workspaceID, func(current geocoding.Settings) (geocoding.Settings, error) {
		return current.Apply(change, editor, uc.configured, uc.deps.Now())
	})
	if err != nil {
		return GeocodingSettingsView{}, err
	}
	return uc.view(ctx, workspaceID, settings, editor)
}

func (uc *GeocodingSettingsUseCase) editor(workspaceID string, caller wsc.Caller) (geocoding.Editor, error) {
	membership, err := ReadMembership(uc.deps.Workspaces, workspaceID, caller.UserID, caller.PlatformAdmin)
	if err != nil {
		return geocoding.Editor{}, err
	}
	return geocoding.Editor{UserID: membership.UserID, OwnerID: membership.OwnerID, PlatformAdmin: membership.PlatformAdmin}, nil
}

func (uc *GeocodingSettingsUseCase) view(ctx context.Context, workspaceID string, settings geocoding.Settings, editor geocoding.Editor) (GeocodingSettingsView, error) {
	view := GeocodingSettingsView{
		Settings:          settings,
		Providers:         append([]geocoding.Provider(nil), uc.deps.Providers...),
		CanChangeProvider: editor.CanChangeProvider(),
		CanChangeCeiling:  editor.CanChangeCeiling(),
	}
	names := uc.deps.Names.Names(settings.ProviderChangedBy, settings.CeilingChangedBy)
	view.ProviderChangedByName, view.CeilingChangedByName = names[settings.ProviderChangedBy], names[settings.CeilingChangedBy]
	uc.readPause(ctx, settings, &view)
	slot, err := settings.SlotAt(uc.deps.Now())
	if err != nil {
		return view, nil
	}
	usage, err := uc.deps.Usage.Usage(ctx, workspaceID)
	if err != nil {
		return GeocodingSettingsView{}, err
	}
	view.Slot, view.Usage = &slot, usage.In(slot)
	return view, nil
}

func (uc *GeocodingSettingsUseCase) readPause(ctx context.Context, settings geocoding.Settings, view *GeocodingSettingsView) {
	if !settings.Enabled() {
		return
	}
	pause, open, err := uc.deps.Pauses.ProviderPause(ctx, settings.Provider)
	if err != nil {
		view.PauseUnreadable = true
		return
	}
	if open && pause.ActiveAt(uc.deps.Now()) {
		view.Pause = &pause
	}
}
