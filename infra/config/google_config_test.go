package config

import (
	"strings"
	"testing"
)

func setGoogleEnv(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"GOOGLE_OAUTH_CLIENT_ID":                  "client",
		"GOOGLE_OAUTH_CLIENT_SECRET":              "secret",
		"GOOGLE_OAUTH_REDIRECT_URI":               "https://api.example.com/oauth/google/callback",
		"GOOGLE_SHEETS_PROJECT_WRITES_PER_MINUTE": "300",
		"GOOGLE_SHEETS_PROJECT_READS_PER_MINUTE":  "300",
		"GOOGLE_SHEETS_USER_WRITES_PER_MINUTE":    "60",
		"GOOGLE_SHEETS_USER_READS_PER_MINUTE":     "60",
		"GOOGLE_SHEETS_DISPATCH_CONCURRENCY":      "8",
	} {
		t.Setenv(key, value)
	}
}

func TestGoogleConfigReadsEveryVariable(t *testing.T) {
	setGoogleEnv(t)
	cfg, missing := readGoogleConfig()
	if len(missing) != 0 {
		t.Fatalf("missing = %v", missing)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.SheetsProjectWritesPerMinute != 300 || cfg.SheetsUserWritesPerMinute != 60 || cfg.SheetsDispatchConcurrency != 8 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestGoogleConfigReportsMissingVariables(t *testing.T) {
	setGoogleEnv(t)
	t.Setenv("GOOGLE_OAUTH_REDIRECT_URI", " ")
	t.Setenv("GOOGLE_SHEETS_DISPATCH_CONCURRENCY", "")
	_, missing := readGoogleConfig()
	if strings.Join(missing, ",") != "GOOGLE_OAUTH_REDIRECT_URI,GOOGLE_SHEETS_DISPATCH_CONCURRENCY" {
		t.Fatalf("missing = %v", missing)
	}
}

func TestGoogleConfigRefusesQuotasThatContradictGoogle(t *testing.T) {
	cases := map[string]string{
		"GOOGLE_SHEETS_USER_WRITES_PER_MINUTE":    "600",
		"GOOGLE_SHEETS_PROJECT_READS_PER_MINUTE":  "abc",
		"GOOGLE_SHEETS_PROJECT_WRITES_PER_MINUTE": "0",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			setGoogleEnv(t)
			t.Setenv(key, value)
			cfg, _ := readGoogleConfig()
			if err := cfg.Validate(); err == nil {
				t.Fatalf("%s=%s accepted", key, value)
			}
		})
	}
}
