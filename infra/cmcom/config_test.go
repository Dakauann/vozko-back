package cmcom

import (
	"errors"
	"testing"
)

const (
	testToken   = "6e2b1a40-1f6c-4d2a-9c3b-0a1b2c3d4e5f"
	testAccount = "0f4c2d1e-8a7b-4c6d-9e5f-112233445566"
)

func TestConfigValidate(t *testing.T) {
	valid := Config{ProductToken: testToken, AccountID: testAccount}
	cases := []struct {
		name   string
		mutate func(*Config)
		ok     bool
	}{
		{"defaults", func(*Config) {}, true},
		{"explicit https urls", func(c *Config) {
			c.GatewayURL = "https://gw.messaging.cm.com"
			c.APIURL = "https://api.cm.com"
		}, true},
		{"loopback fake over http", func(c *Config) {
			c.GatewayURL = "http://127.0.0.1:50123"
			c.APIURL = "http://localhost:50123"
		}, true},
		{"ipv6 loopback over http", func(c *Config) { c.GatewayURL = "http://[::1]:8080" }, true},
		{"missing token", func(c *Config) { c.ProductToken = " " }, false},
		{"token without hyphens", func(c *Config) { c.ProductToken = "6e2b1a401f6c4d2a9c3b0a1b2c3d4e5f" }, false},
		{"missing account", func(c *Config) { c.AccountID = "" }, false},
		{"account not a guid", func(c *Config) { c.AccountID = "account-1" }, false},
		{"plain http gateway", func(c *Config) { c.GatewayURL = "http://gw.messaging.cm.com" }, false},
		{"plain http api", func(c *Config) { c.APIURL = "http://api.cm.com" }, false},
		{"localhost lookalike", func(c *Config) { c.APIURL = "http://localhost.evil.example" }, false},
		{"not a url", func(c *Config) { c.GatewayURL = "::nope" }, false},
		{"url with path", func(c *Config) { c.GatewayURL = "https://gw.messaging.cm.com/v1.0" }, false},
		{"url without host", func(c *Config) { c.APIURL = "https://" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.ok && !errors.Is(err, ErrConfigInvalid) {
				t.Fatalf("Validate() = %v, want ErrConfigInvalid", err)
			}
		})
	}
}

func TestNewClientAppliesDefaults(t *testing.T) {
	c, err := NewClient(Config{ProductToken: testToken, AccountID: testAccount})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.gatewayURL != DefaultGatewayURL || c.apiURL != DefaultAPIURL {
		t.Fatalf("urls = %q %q", c.gatewayURL, c.apiURL)
	}
	if c.http == nil || c.http.Timeout == 0 {
		t.Fatalf("expected a default http client with a timeout")
	}
	if c.Name() != "cmcom" {
		t.Fatalf("Name() = %q", c.Name())
	}
	if _, err := NewClient(Config{ProductToken: testToken}); !errors.Is(err, ErrConfigInvalid) {
		t.Fatalf("NewClient without account = %v", err)
	}
}
