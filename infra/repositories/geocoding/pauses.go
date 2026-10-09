package geocoding_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
)

const pauseKeyPrefix = "geocoding:provider_pause:"

var (
	errPauseStateMissing = errors.New("geocoding pause: no shared state store")
	errPauseUnreadable   = errors.New("geocoding pause: the stored pause is not readable")
	errPauseInvalid      = errors.New("geocoding pause: only a valid account pause is shared")
)

type storedPause struct {
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
}

type PauseStore struct {
	state cache.SharedState
	now   func() time.Time
}

var _ geocoding.PauseStore = (*PauseStore)(nil)

func NewPauseStore(state cache.SharedState) *PauseStore {
	return &PauseStore{state: state, now: func() time.Time { return time.Now().UTC() }}
}

func (s *PauseStore) ProviderPause(_ context.Context, provider geocoding.Provider) (geocoding.ProviderPause, bool, error) {
	if s.state == nil {
		return geocoding.ProviderPause{}, false, errPauseStateMissing
	}
	raw, err := s.state.GetString(pauseKeyPrefix + string(provider))
	if err != nil {
		return geocoding.ProviderPause{}, false, err
	}
	if raw == "" {
		return geocoding.ProviderPause{}, false, nil
	}
	var stored storedPause
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return geocoding.ProviderPause{}, false, fmt.Errorf("%w: %v", errPauseUnreadable, err)
	}
	pause := geocoding.ProviderPause{Provider: provider, Reason: geo.UnavailableReason(stored.Reason), Since: stored.Since, Until: stored.Until}
	if !pause.Valid() {
		return geocoding.ProviderPause{}, false, errPauseUnreadable
	}
	return pause, pause.ActiveAt(s.now()), nil
}

func (s *PauseStore) OpenProviderPause(_ context.Context, pause geocoding.ProviderPause) error {
	if s.state == nil {
		return errPauseStateMissing
	}
	if !pause.Valid() {
		return errPauseInvalid
	}
	ttl := pause.Remaining(s.now())
	if ttl <= 0 {
		return nil
	}
	raw, err := json.Marshal(storedPause{Reason: string(pause.Reason), Since: pause.Since.UTC(), Until: pause.Until.UTC()})
	if err != nil {
		return err
	}
	return s.state.SetString(pauseKeyPrefix+string(pause.Provider), string(raw), ttl)
}
