package main

import "testing"

const (
	testToken   = "9f8e7d6c-5b4a-4392-8170-6f5e4d3c2b1a"
	testAccount = "1a2b3c4d-5e6f-4071-8293-a4b5c6d7e8f9"
	testBase    = "http://127.0.0.1:4000/webhooks/carrier/cmcom/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
)

func TestLoadConfig(t *testing.T) {
	full := map[string]string{
		"CMFAKE_PRODUCT_TOKEN": testToken,
		"CMFAKE_ACCOUNT_ID":    testAccount,
		"CMFAKE_WEBHOOK_BASE":  testBase + "/",
	}
	cases := []struct {
		name   string
		env    map[string]string
		ok     bool
		addr   string
		mutate func(map[string]string)
	}{
		{name: "defaults the address", ok: true, addr: defaultAddr},
		{name: "custom address", ok: true, addr: "0.0.0.0:9000", mutate: func(e map[string]string) { e["CMFAKE_ADDR"] = "0.0.0.0:9000" }},
		{name: "missing token", mutate: func(e map[string]string) { delete(e, "CMFAKE_PRODUCT_TOKEN") }},
		{name: "token not a guid", mutate: func(e map[string]string) { e["CMFAKE_PRODUCT_TOKEN"] = "secret" }},
		{name: "missing account", mutate: func(e map[string]string) { delete(e, "CMFAKE_ACCOUNT_ID") }},
		{name: "missing webhook base", mutate: func(e map[string]string) { delete(e, "CMFAKE_WEBHOOK_BASE") }},
		{name: "webhook base not a url", mutate: func(e map[string]string) { e["CMFAKE_WEBHOOK_BASE"] = "127.0.0.1:4000/webhooks" }},
		{name: "webhook base with query", mutate: func(e map[string]string) { e["CMFAKE_WEBHOOK_BASE"] = testBase + "?x=1" }},
		{name: "address without port", mutate: func(e map[string]string) { e["CMFAKE_ADDR"] = "127.0.0.1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range full {
				env[k] = v
			}
			if tc.mutate != nil {
				tc.mutate(env)
			}
			cfg, err := loadConfig(func(key string) string { return env[key] })
			if !tc.ok {
				if err == nil {
					t.Fatalf("loadConfig must fail, got %+v", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig: %v", err)
			}
			if cfg.addr != tc.addr || cfg.productToken != testToken || cfg.accountID != testAccount || cfg.webhookBase != testBase {
				t.Fatalf("cfg = %+v", cfg)
			}
		})
	}
}
