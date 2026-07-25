package workspace_config

import "testing"

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
