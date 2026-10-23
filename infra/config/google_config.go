package config

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

type GoogleConfig struct {
	ClientID                     string
	ClientSecret                 string
	RedirectURI                  string
	SheetsProjectWritesPerMinute int
	SheetsProjectReadsPerMinute  int
	SheetsUserWritesPerMinute    int
	SheetsUserReadsPerMinute     int
	SheetsDispatchConcurrency    int
}

func loadGoogleConfig() GoogleConfig {
	cfg, missing := readGoogleConfig()
	if len(missing) > 0 {
		log.Fatalf("required env vars are not set: %s", strings.Join(missing, ", "))
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid Google configuration: %v", err)
	}
	return cfg
}

func readGoogleConfig() (GoogleConfig, []string) {
	var missing []string
	required := func(key string) string {
		v := strings.TrimSpace(trimEnv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	count := func(key string) int {
		raw := required(key)
		if raw == "" {
			return 0
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return -1
		}
		return n
	}
	cfg := GoogleConfig{
		ClientID:                     required("GOOGLE_OAUTH_CLIENT_ID"),
		ClientSecret:                 required("GOOGLE_OAUTH_CLIENT_SECRET"),
		RedirectURI:                  required("GOOGLE_OAUTH_REDIRECT_URI"),
		SheetsProjectWritesPerMinute: count("GOOGLE_SHEETS_PROJECT_WRITES_PER_MINUTE"),
		SheetsProjectReadsPerMinute:  count("GOOGLE_SHEETS_PROJECT_READS_PER_MINUTE"),
		SheetsUserWritesPerMinute:    count("GOOGLE_SHEETS_USER_WRITES_PER_MINUTE"),
		SheetsUserReadsPerMinute:     count("GOOGLE_SHEETS_USER_READS_PER_MINUTE"),
		SheetsDispatchConcurrency:    count("GOOGLE_SHEETS_DISPATCH_CONCURRENCY"),
	}
	return cfg, missing
}

func (c GoogleConfig) Validate() error {
	positive := map[string]int{
		"GOOGLE_SHEETS_PROJECT_WRITES_PER_MINUTE": c.SheetsProjectWritesPerMinute,
		"GOOGLE_SHEETS_PROJECT_READS_PER_MINUTE":  c.SheetsProjectReadsPerMinute,
		"GOOGLE_SHEETS_USER_WRITES_PER_MINUTE":    c.SheetsUserWritesPerMinute,
		"GOOGLE_SHEETS_USER_READS_PER_MINUTE":     c.SheetsUserReadsPerMinute,
		"GOOGLE_SHEETS_DISPATCH_CONCURRENCY":      c.SheetsDispatchConcurrency,
	}
	for key, value := range positive {
		if value <= 0 {
			return fmt.Errorf("%s must be a positive integer", key)
		}
	}
	if c.SheetsUserWritesPerMinute > c.SheetsProjectWritesPerMinute {
		return fmt.Errorf("GOOGLE_SHEETS_USER_WRITES_PER_MINUTE cannot exceed GOOGLE_SHEETS_PROJECT_WRITES_PER_MINUTE")
	}
	if c.SheetsUserReadsPerMinute > c.SheetsProjectReadsPerMinute {
		return fmt.Errorf("GOOGLE_SHEETS_USER_READS_PER_MINUTE cannot exceed GOOGLE_SHEETS_PROJECT_READS_PER_MINUTE")
	}
	return nil
}
