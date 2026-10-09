package workspace_config_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/geocoding"
	wsc "vozko/domain/workspace_config"
)

type memGeocodingStore struct {
	settings geocoding.Settings
	readErr  error
	changes  int
}

func (s *memGeocodingStore) Settings(context.Context, string) (geocoding.Settings, error) {
	return s.settings, s.readErr
}

func (s *memGeocodingStore) SettingsOf(context.Context, []string) (map[string]geocoding.Settings, error) {
	return nil, errors.New("not used")
}

func (s *memGeocodingStore) ChangeSettings(_ context.Context, _ string, change func(geocoding.Settings) (geocoding.Settings, error)) (geocoding.Settings, error) {
	next, err := change(s.settings)
	if err != nil {
		return geocoding.Settings{}, err
	}
	s.changes++
	s.settings = next
	return next, nil
}

type memGeocodingUsage struct {
	usage geocoding.Usage
	err   error
}

func (u memGeocodingUsage) TakeSlot(context.Context, string, geocoding.Slot) (bool, geocoding.Usage, error) {
	return false, geocoding.Usage{}, errors.New("not used")
}

func (u memGeocodingUsage) Usage(context.Context, string) (geocoding.Usage, error) {
	return u.usage, u.err
}

func newGeocodingUseCase(t *testing.T, store *memGeocodingStore, usage memGeocodingUsage, providers ...geocoding.Provider) *GeocodingSettingsUseCase {
	t.Helper()
	return pausedGeocodingUseCase(t, store, usage, &memPauses{}, providers...)
}

func pausedGeocodingUseCase(t *testing.T, store *memGeocodingStore, usage memGeocodingUsage, pauses *memPauses, providers ...geocoding.Provider) *GeocodingSettingsUseCase {
	t.Helper()
	workspaces := &memWorkspaces{ownerID: "owner-1", members: map[string]bool{"owner-1": true, "member-1": true}}
	uc, err := NewGeocodingSettingsUseCase(GeocodingSettingsDeps{
		Store: store, Usage: usage, Pauses: pauses, Workspaces: workspaces, Names: memNames{"owner-1": "Ana Dona"},
		Providers: providers, Now: func() time.Time { return policyNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return uc
}

func TestNewGeocodingSettingsUseCaseRefusesMissingDependencies(t *testing.T) {
	_, err := NewGeocodingSettingsUseCase(GeocodingSettingsDeps{Usage: memGeocodingUsage{}, Pauses: &memPauses{}, Workspaces: &memWorkspaces{}, Names: memNames{}})
	if !errors.Is(err, errGeocodingSettingsIncomplete) || !strings.Contains(err.Error(), "store") {
		t.Fatalf("a use case without its store = %v, want the missing dependency named", err)
	}
	_, err = NewGeocodingSettingsUseCase(GeocodingSettingsDeps{Store: &memGeocodingStore{}, Usage: memGeocodingUsage{}, Workspaces: &memWorkspaces{}, Names: memNames{}})
	if !errors.Is(err, errGeocodingSettingsIncomplete) || !strings.Contains(err.Error(), "pauses") {
		t.Fatalf("a use case without the provider pause = %v, want the missing dependency named", err)
	}
}

func TestGeocodingSettingsViewForAMember(t *testing.T) {
	opted := policyNow.Add(-time.Hour)
	store := &memGeocodingStore{settings: geocoding.Settings{Provider: geocoding.ProviderOpenCage, ProviderChangedBy: "owner-1", ProviderChangedAt: &opted}}
	slot, _ := store.settings.SlotAt(policyNow)
	usage := memGeocodingUsage{usage: geocoding.Usage{CycleStart: slot.CycleStart, Requests: 120, Day: slot.Day.AddDate(0, 0, -1), DayRequests: 40}}
	view, err := newGeocodingUseCase(t, store, usage, geocoding.ProviderOpenCage).Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "member-1"})
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if view.ProviderChangedByName != "Ana Dona" || view.CanChangeProvider || view.CanChangeCeiling {
		t.Fatalf("view = %+v, want the name of who opted in and no edit rights for a member", view)
	}
	if view.Slot == nil || view.Usage.Requests != 120 || view.Usage.DayRequests != 0 {
		t.Fatalf("usage = %+v, slot = %+v, want this cycle's requests and yesterday's day count reset", view.Usage, view.Slot)
	}
	if len(view.Providers) != 1 || view.Providers[0] != geocoding.ProviderOpenCage {
		t.Fatalf("providers = %v, want the server's providers", view.Providers)
	}
}

func TestGeocodingSettingsRefusals(t *testing.T) {
	store := &memGeocodingStore{}
	uc := newGeocodingUseCase(t, store, memGeocodingUsage{}, geocoding.ProviderOpenCage)
	opencage := geocoding.ProviderOpenCage
	if _, err := uc.Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "stranger"}); !errors.Is(err, ErrNotWorkspaceMember) {
		t.Fatalf("a stranger reading = %v, want not a member", err)
	}
	if _, err := uc.Change(context.Background(), policyWorkspace, wsc.Caller{UserID: "member-1"}, geocoding.Change{Provider: &opencage}); !errors.Is(err, geocoding.ErrSettingsForbidden) {
		t.Fatalf("a member opting in = %v, want forbidden", err)
	}
	if _, err := uc.Get(context.Background(), "not-a-uuid", wsc.Caller{UserID: "owner-1"}); !errors.Is(err, ErrNotWorkspaceMember) {
		t.Fatalf("a malformed workspace = %v, want not a member", err)
	}
	if store.changes != 0 {
		t.Fatal("a refused change was written")
	}
	store.readErr = errors.New("db down")
	if _, err := uc.Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "owner-1"}); err == nil || errors.Is(err, geocoding.ErrSettingsForbidden) {
		t.Fatalf("an unreadable store = %v, want the read error, never an empty answer", err)
	}
}

func TestGeocodingSettingsUsageErrorIsNotZeroUsage(t *testing.T) {
	store := &memGeocodingStore{settings: geocoding.Settings{Provider: geocoding.ProviderOpenCage}}
	uc := newGeocodingUseCase(t, store, memGeocodingUsage{err: errors.New("db down")}, geocoding.ProviderOpenCage)
	if _, err := uc.Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "owner-1"}); err == nil {
		t.Fatal("an unreadable usage must not be shown as zero")
	}
}

func TestTheOwnerOptsInAndAPlatformAdminSetsTheCeiling(t *testing.T) {
	store := &memGeocodingStore{}
	uc := newGeocodingUseCase(t, store, memGeocodingUsage{}, geocoding.ProviderOpenCage)
	opencage := geocoding.ProviderOpenCage
	view, err := uc.Change(context.Background(), policyWorkspace, wsc.Caller{UserID: "owner-1"}, geocoding.Change{Provider: &opencage})
	if err != nil || !view.Settings.Enabled() || !view.CanChangeProvider || view.CanChangeCeiling {
		t.Fatalf("Change() = %+v, %v, want the opt-in by the owner", view, err)
	}
	ceiling := int64(800)
	view, err = uc.Change(context.Background(), policyWorkspace, wsc.Caller{UserID: "staff-1", PlatformAdmin: true}, geocoding.Change{MonthlyCeiling: &ceiling})
	if err != nil || view.Settings.Ceiling() != 800 || !view.CanChangeCeiling {
		t.Fatalf("Change() = %+v, %v, want the ceiling set by staff", view, err)
	}
}

func TestOptingIntoAProviderTheServerLacksIsRefused(t *testing.T) {
	uc := newGeocodingUseCase(t, &memGeocodingStore{}, memGeocodingUsage{})
	opencage := geocoding.ProviderOpenCage
	if _, err := uc.Change(context.Background(), policyWorkspace, wsc.Caller{UserID: "owner-1"}, geocoding.Change{Provider: &opencage}); !errors.Is(err, geocoding.ErrProviderNotConfigured) {
		t.Fatalf("Change() err = %v, want ErrProviderNotConfigured", err)
	}
}

type memPauses struct {
	pause *geocoding.ProviderPause
	err   error
	asked []geocoding.Provider
}

func (p *memPauses) ProviderPause(_ context.Context, provider geocoding.Provider) (geocoding.ProviderPause, bool, error) {
	p.asked = append(p.asked, provider)
	if p.err != nil {
		return geocoding.ProviderPause{}, false, p.err
	}
	if p.pause == nil {
		return geocoding.ProviderPause{}, false, nil
	}
	return *p.pause, true, nil
}

func TestGeocodingSettingsTellTheTruthAboutAPausedProvider(t *testing.T) {
	store := &memGeocodingStore{settings: geocoding.Settings{Provider: geocoding.ProviderOpenCage}}
	since := policyNow.Add(-10 * time.Minute)
	pause := geocoding.ProviderPause{Provider: geocoding.ProviderOpenCage, Reason: "key_rejected", Since: since, Until: since.Add(geocoding.AccountPause)}
	pauses := &memPauses{pause: &pause}
	view, err := pausedGeocodingUseCase(t, store, memGeocodingUsage{}, pauses, geocoding.ProviderOpenCage).Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "member-1"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Pause == nil || view.Pause.Reason != "key_rejected" || !view.Pause.Since.Equal(since) || view.PauseUnreadable {
		t.Fatalf("view = %+v, want the pause with its reason and since when", view)
	}
}

func TestGeocodingSettingsHideAPauseThatEndedOrAProviderThatIsOff(t *testing.T) {
	ended := geocoding.ProviderPause{Provider: geocoding.ProviderOpenCage, Reason: "key_rejected", Since: policyNow.Add(-2 * time.Hour), Until: policyNow.Add(-time.Hour)}
	view, err := pausedGeocodingUseCase(t, &memGeocodingStore{settings: geocoding.Settings{Provider: geocoding.ProviderOpenCage}}, memGeocodingUsage{}, &memPauses{pause: &ended}, geocoding.ProviderOpenCage).
		Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "member-1"})
	if err != nil || view.Pause != nil || view.PauseUnreadable {
		t.Fatalf("view = %+v, %v, want no pause once it ended", view, err)
	}
	off := &memPauses{err: errors.New("redis down")}
	view, err = pausedGeocodingUseCase(t, &memGeocodingStore{}, memGeocodingUsage{}, off, geocoding.ProviderOpenCage).
		Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "member-1"})
	if err != nil || view.Pause != nil || view.PauseUnreadable || len(off.asked) != 0 {
		t.Fatalf("view = %+v, %v, asked %v, want the pause never read for a workspace without a provider", view, err, off.asked)
	}
}

func TestGeocodingSettingsSayWhenThePauseCannotBeRead(t *testing.T) {
	store := &memGeocodingStore{settings: geocoding.Settings{Provider: geocoding.ProviderOpenCage}}
	view, err := pausedGeocodingUseCase(t, store, memGeocodingUsage{}, &memPauses{err: errors.New("redis down")}, geocoding.ProviderOpenCage).
		Get(context.Background(), policyWorkspace, wsc.Caller{UserID: "member-1"})
	if err != nil || !view.PauseUnreadable || view.Pause != nil {
		t.Fatalf("view = %+v, %v, want the card told the provider state is unknown", view, err)
	}
}
