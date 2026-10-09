package geocoding_repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
)

type memoryState struct {
	cache.SharedState
	values  map[string]string
	ttls    map[string]time.Duration
	readErr error
	setErr  error
}

func newMemoryState() *memoryState {
	return &memoryState{values: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (m *memoryState) GetString(key string) (string, error) {
	if m.readErr != nil {
		return "", m.readErr
	}
	return m.values[key], nil
}

func (m *memoryState) SetString(key, value string, ttl time.Duration) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.values[key], m.ttls[key] = value, ttl
	return nil
}

var pausedAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func pauseStore(state cache.SharedState, now time.Time) *PauseStore {
	s := NewPauseStore(state)
	s.now = func() time.Time { return now }
	return s
}

func TestAPauseIsSharedThroughTheStateStoreAndExpiresOnItsOwn(t *testing.T) {
	state := newMemoryState()
	pause, _ := geocoding.PauseAfter(geocoding.ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), pausedAt)
	if err := pauseStore(state, pausedAt).OpenProviderPause(context.Background(), pause); err != nil {
		t.Fatal(err)
	}
	if ttl := state.ttls[pauseKeyPrefix+"opencage"]; ttl != geocoding.AccountPause {
		t.Fatalf("ttl = %v, want the pause to expire by itself after %v", ttl, geocoding.AccountPause)
	}
	got, open, err := pauseStore(state, pausedAt.Add(10*time.Minute)).ProviderPause(context.Background(), geocoding.ProviderOpenCage)
	if err != nil || !open || got.Reason != geo.ReasonKeyRejected || !got.Since.Equal(pausedAt) || !got.Until.Equal(pause.Until) || got.Provider != geocoding.ProviderOpenCage {
		t.Fatalf("ProviderPause() = %+v, %v, %v, want the shared pause", got, open, err)
	}
	if _, open, err := pauseStore(state, pause.Until).ProviderPause(context.Background(), geocoding.ProviderOpenCage); err != nil || open {
		t.Fatalf("a pause past its end = %v, %v, want closed", open, err)
	}
}

func TestNoPauseStoredMeansTheProviderIsUsable(t *testing.T) {
	_, open, err := pauseStore(newMemoryState(), pausedAt).ProviderPause(context.Background(), geocoding.ProviderOpenCage)
	if err != nil || open {
		t.Fatalf("ProviderPause() = %v, %v, want no pause", open, err)
	}
}

func TestAnUnreadablePauseIsAnErrorNeverNoPause(t *testing.T) {
	tests := []struct {
		name  string
		state *memoryState
	}{
		{"the state store is down", &memoryState{values: map[string]string{}, readErr: errors.New("redis down")}},
		{"the stored value is not a pause", &memoryState{values: map[string]string{pauseKeyPrefix + "opencage": "{"}}},
		{"the stored pause has no end", &memoryState{values: map[string]string{pauseKeyPrefix + "opencage": `{"reason":"key_rejected","since":"2026-10-08T12:00:00Z"}`}}},
		{"the stored pause has a foreign reason", &memoryState{values: map[string]string{pauseKeyPrefix + "opencage": `{"reason":"maintenance","since":"2026-10-08T12:00:00Z","until":"2026-10-08T13:00:00Z"}`}}},
		{"the stored pause has a transient reason", &memoryState{values: map[string]string{pauseKeyPrefix + "opencage": `{"reason":"provider_down","since":"2026-10-08T12:00:00Z","until":"2026-10-08T13:00:00Z"}`}}},
		{"the stored pause has no start", &memoryState{values: map[string]string{pauseKeyPrefix + "opencage": `{"reason":"key_rejected","until":"2026-10-08T13:00:00Z"}`}}},
		{"the stored pause ends before it starts", &memoryState{values: map[string]string{pauseKeyPrefix + "opencage": `{"reason":"key_rejected","since":"2026-10-08T13:00:00Z","until":"2026-10-08T12:30:00Z"}`}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := pauseStore(tt.state, pausedAt).ProviderPause(context.Background(), geocoding.ProviderOpenCage); err == nil {
				t.Fatal("an unreadable pause must be an error so no call is made")
			}
		})
	}
	missing := NewPauseStore(nil)
	if _, _, err := missing.ProviderPause(context.Background(), geocoding.ProviderOpenCage); err == nil {
		t.Fatal("a pause store without a state store must refuse")
	}
	if err := missing.OpenProviderPause(context.Background(), geocoding.ProviderPause{Provider: geocoding.ProviderOpenCage, Until: pausedAt.Add(time.Hour)}); err == nil {
		t.Fatal("a pause store without a state store must say the pause was not shared")
	}
}

func TestAnEndedPauseIsNotWritten(t *testing.T) {
	state := newMemoryState()
	ended := geocoding.ProviderPause{Provider: geocoding.ProviderOpenCage, Reason: geo.ReasonKeyRejected, Since: pausedAt.Add(-2 * time.Hour), Until: pausedAt.Add(-time.Hour)}
	if err := pauseStore(state, pausedAt).OpenProviderPause(context.Background(), ended); err != nil {
		t.Fatal(err)
	}
	if len(state.values) != 0 {
		t.Fatalf("stored %+v, want nothing for a pause that already ended", state.values)
	}
}

func TestAnInvalidPauseIsNeverWritten(t *testing.T) {
	valid, _ := geocoding.PauseAfter(geocoding.ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), pausedAt)
	tests := []struct {
		name string
		edit func(p *geocoding.ProviderPause)
	}{
		{"a foreign reason", func(p *geocoding.ProviderPause) { p.Reason = "maintenance" }},
		{"a transient reason", func(p *geocoding.ProviderPause) { p.Reason = geo.ReasonProviderDown }},
		{"an unknown provider", func(p *geocoding.ProviderPause) { p.Provider = "nominatim" }},
		{"no start", func(p *geocoding.ProviderPause) { p.Since = time.Time{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newMemoryState()
			pause := valid
			tt.edit(&pause)
			if err := pauseStore(state, pausedAt).OpenProviderPause(context.Background(), pause); err == nil {
				t.Fatal("an invalid pause must be refused")
			}
			if len(state.values) != 0 {
				t.Fatalf("stored %+v, want nothing", state.values)
			}
		})
	}
}
