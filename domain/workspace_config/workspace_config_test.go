package workspace_config

import (
	"testing"
	"time"
)

func TestClampAutoCloseIdleHours(t *testing.T) {
	if got := ClampAutoCloseIdleHours(0); got != DefaultAutoCloseIdleAfterHours {
		t.Fatalf("got %d", got)
	}
	if got := ClampAutoCloseIdleHours(24); got != 24 {
		t.Fatalf("got %d", got)
	}
	if got := ClampAutoCloseIdleHours(200); got != MaxAutoCloseIdleAfterHours {
		t.Fatalf("got %d", got)
	}
}

func TestDefaults(t *testing.T) {
	if !DefaultAutoCloseEnabled {
		t.Fatal("auto close must default enabled")
	}
	if DefaultAutoCloseIdleAfterHours != 24 {
		t.Fatalf("default hours %d", DefaultAutoCloseIdleAfterHours)
	}
	if !DefaultAutoCloseMaxAgeEnabled {
		t.Fatal("max-age must default enabled (hygiene floor)")
	}
	if DefaultAutoCloseMaxAgeAfterHours != 168 {
		t.Fatalf("default max-age hours %d want 168", DefaultAutoCloseMaxAgeAfterHours)
	}
}

func TestClampAutoCloseMaxAgeHours(t *testing.T) {
	if got := ClampAutoCloseMaxAgeHours(0); got != DefaultAutoCloseMaxAgeAfterHours {
		t.Fatalf("0 → %d", got)
	}
	if got := ClampAutoCloseMaxAgeHours(48); got != 48 {
		t.Fatalf("48 → %d", got)
	}
	if got := ClampAutoCloseMaxAgeHours(99999); got != MaxAutoCloseMaxAgeAfterHours {
		t.Fatalf("huge → %d", got)
	}
}

func TestEffectiveAutoCloseIdleAfterHours(t *testing.T) {
	var nilCfg *WorkspaceConfig
	if nilCfg.EffectiveAutoCloseIdleAfterHours() != DefaultAutoCloseIdleAfterHours {
		t.Fatal("nil config")
	}
	cfg := &WorkspaceConfig{AutoCloseIdleAfterHours: 12}
	if cfg.EffectiveAutoCloseIdleAfterHours() != 12 {
		t.Fatal("12")
	}
}

func TestNormalizeRouletteMode(t *testing.T) {
	cases := map[string]string{
		RouletteModeOnline:   RouletteModeOnline,
		RouletteModeLastSeen: RouletteModeLastSeen,
		"":                   DefaultRouletteMode,
		"onlinne":            DefaultRouletteMode,
		"LAST_SEEN":          DefaultRouletteMode,
		"last-seen":          DefaultRouletteMode,
	}
	for in, want := range cases {
		if got := NormalizeRouletteMode(in); got != want {
			t.Fatalf("NormalizeRouletteMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampRouletteLastSeenWindowHours(t *testing.T) {
	cases := map[int]int{
		0:    DefaultRouletteLastSeenWindowHours,
		-5:   DefaultRouletteLastSeenWindowHours,
		1:    1,
		48:   48,
		168:  168,
		9999: MaxRouletteLastSeenWindowHours,
	}
	for in, want := range cases {
		if got := ClampRouletteLastSeenWindowHours(in); got != want {
			t.Fatalf("ClampRouletteLastSeenWindowHours(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestClampRouletteRescueMinutes(t *testing.T) {
	cases := map[int]int{
		0:      DefaultRouletteRescueAfterMinutes,
		-1:     DefaultRouletteRescueAfterMinutes,
		1:      1,
		15:     15,
		1440:   1440,
		100000: MaxRouletteRescueAfterMinutes,
	}
	for in, want := range cases {
		if got := ClampRouletteRescueMinutes(in); got != want {
			t.Fatalf("ClampRouletteRescueMinutes(%d) = %d, want %d", in, got, want)
		}
	}
}

// The guard that keeps this feature dark on upgrade: every existing workspace
// must land on the historical mode.
func TestRouletteDefaultsAreTheHistoricalBehaviour(t *testing.T) {
	if DefaultRouletteMode != RouletteModeOnline {
		t.Fatal("the default roulette mode must stay online, or an upgrade silently changes distribution for every workspace")
	}
	var nilCfg *WorkspaceConfig
	if nilCfg.EffectiveRouletteMode() != RouletteModeOnline {
		t.Fatal("a nil config must resolve to the online mode")
	}
	if nilCfg.RouletteRescueActive() {
		t.Fatal("a nil config must not activate rescue")
	}
}

func TestEffectiveRouletteHelpers(t *testing.T) {
	var nilCfg *WorkspaceConfig
	if got := nilCfg.EffectiveRouletteLastSeenWindow(); got != 48*time.Hour {
		t.Fatalf("nil window = %s", got)
	}
	if got := nilCfg.EffectiveRouletteRescueAfter(); got != 15*time.Minute {
		t.Fatalf("nil rescue = %s", got)
	}

	cfg := &WorkspaceConfig{RouletteLastSeenWindowHours: 9999, RouletteRescueAfterMinutes: 0}
	if got := cfg.EffectiveRouletteLastSeenWindow(); got != 168*time.Hour {
		t.Fatalf("clamped window = %s", got)
	}
	if got := cfg.EffectiveRouletteRescueAfter(); got != 15*time.Minute {
		t.Fatalf("clamped rescue = %s", got)
	}
}

// E10: rescue is gated on the mode, not only on its own toggle.
func TestRouletteRescueActive(t *testing.T) {
	cases := []struct {
		mode    string
		enabled bool
		want    bool
	}{
		{RouletteModeLastSeen, true, true},
		{RouletteModeLastSeen, false, false},
		{RouletteModeOnline, true, false},
		{RouletteModeOnline, false, false},
		{"garbage", true, false},
	}
	for _, tc := range cases {
		cfg := &WorkspaceConfig{RouletteMode: tc.mode, RouletteRescueEnabled: tc.enabled}
		if got := cfg.RouletteRescueActive(); got != tc.want {
			t.Fatalf("mode=%q enabled=%v: got %v want %v", tc.mode, tc.enabled, got, tc.want)
		}
	}
}
