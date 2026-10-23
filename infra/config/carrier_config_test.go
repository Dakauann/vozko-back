package config

import (
	"strings"
	"testing"
)

func validCarrierConfig() CarrierConfig {
	return CarrierConfig{
		CMComProductToken: "0b7d2a3e-5f4c-4e8b-9a1d-2c3b4a5d6e7f",
		CMComAccountID:    "1c7d2a3e-5f4c-4e8b-9a1d-2c3b4a5d6e7f",
		WebhookToken:      strings.Repeat("a", 43),
		LinkSecret:        strings.Repeat("s", 32),
		SMSSharedNumber:   "29000",
		PublicBaseURL:     "https://api.example.com",
	}
}

func TestValidateCarrierConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CarrierConfig)
		want   string
	}{
		{"valid", func(*CarrierConfig) {}, ""},
		{"weak webhook token", func(c *CarrierConfig) { c.WebhookToken = "short" }, "CARRIER_WEBHOOK_TOKEN"},
		{"webhook token with symbols", func(c *CarrierConfig) { c.WebhookToken = strings.Repeat("a", 42) + "!" }, "CARRIER_WEBHOOK_TOKEN"},
		{"short link secret", func(c *CarrierConfig) { c.LinkSecret = "s" }, "CARRIER_LINK_SECRET"},
		{"shared number with letters", func(c *CarrierConfig) { c.SMSSharedNumber = "29a00" }, "SMS_SHARED_NUMBER"},
		{"plain http base", func(c *CarrierConfig) { c.PublicBaseURL = "http://api.example.com" }, "API_BASE_URL"},
		{"http on localhost for development", func(c *CarrierConfig) { c.PublicBaseURL = "http://localhost:4000" }, ""},
		{"http on the loopback address", func(c *CarrierConfig) { c.PublicBaseURL = "http://127.0.0.1:4100" }, ""},
		{"http on a host named like localhost", func(c *CarrierConfig) { c.PublicBaseURL = "http://localhost.example.com" }, "API_BASE_URL"},
		{"base without a host", func(c *CarrierConfig) { c.PublicBaseURL = "https://" }, "API_BASE_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCarrierConfig()
			tc.mutate(&c)
			err := c.Validate()
			if tc.want == "" && err != nil {
				t.Fatalf("Validate = %v", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Validate = %v, want mention of %s", err, tc.want)
			}
		})
	}
}

func TestReadCarrierConfigListsEveryMissingVariable(t *testing.T) {
	for _, key := range []string{"CARRIER_CMCOM_PRODUCT_TOKEN", "CARRIER_CMCOM_ACCOUNT_ID", "CARRIER_WEBHOOK_TOKEN", "CARRIER_LINK_SECRET", "SMS_SHARED_NUMBER", "API_BASE_URL"} {
		t.Setenv(key, "")
	}
	_, missing := readCarrierConfig()
	if len(missing) != 6 {
		t.Fatalf("missing = %v", missing)
	}
}
