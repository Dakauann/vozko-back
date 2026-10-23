package config

import (
	"strings"
	"testing"
)

func validEmailConfig() EmailConfig {
	return EmailConfig{
		AWSRegion:              "sa-east-1",
		AWSAccountID:           "123456789012",
		AWSAccessKeyID:         "AKIAEXAMPLE",
		AWSSecretAccessKey:     "secret",
		EventsTopicARN:         "arn:aws:sns:sa-east-1:123456789012:email-events",
		InboundTopicARN:        "arn:aws:sns:sa-east-1:123456789012:email-inbound",
		AllowedTopicARNs:       []string{"arn:aws:sns:sa-east-1:123456789012:email-events", "arn:aws:sns:sa-east-1:123456789012:email-inbound"},
		InboundBucket:          "inbound-bucket",
		SharedProbationDomain:  "envios.example.com.br",
		SharedTrustedDomain:    "mail.example.com.br",
		Route53ProbationZoneID: "Z09850153216WRIE11RKP",
		Route53TrustedZoneID:   "Z06895732Z3BD5VGPWD6P",
		TokenSecret:            strings.Repeat("k", 32),
		PostmasterClientID:     "client",
		PostmasterClientSecret: "client-secret",
		PostmasterRefreshToken: "refresh",
		PublicBaseURL:          "https://api.example.com",
	}
}

func TestValidateEmailConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EmailConfig)
		want   string
	}{
		{"valid", func(*EmailConfig) {}, ""},
		{"short token secret", func(c *EmailConfig) { c.TokenSecret = "short" }, "EMAIL_TOKEN_SECRET"},
		{"events topic not allowed", func(c *EmailConfig) { c.AllowedTopicARNs = c.AllowedTopicARNs[1:] }, "EMAIL_SNS_EVENTS_TOPIC_ARN"},
		{"inbound topic not allowed", func(c *EmailConfig) { c.AllowedTopicARNs = c.AllowedTopicARNs[:1] }, "EMAIL_SNS_INBOUND_TOPIC_ARN"},
		{"topic not sns", func(c *EmailConfig) {
			c.AllowedTopicARNs = append(c.AllowedTopicARNs, "arn:aws:sqs:sa-east-1:123456789012:q")
		}, "EMAIL_SNS_ALLOWED_TOPIC_ARNS"},
		{"topic in another region", func(c *EmailConfig) {
			c.AllowedTopicARNs = append(c.AllowedTopicARNs, "arn:aws:sns:us-east-1:123456789012:other")
		}, "EMAIL_SNS_ALLOWED_TOPIC_ARNS"},
		{"bad account", func(c *EmailConfig) { c.AWSAccountID = "12" }, "EMAIL_AWS_ACCOUNT_ID"},
		{"bad region", func(c *EmailConfig) { c.AWSRegion = "sa east" }, "EMAIL_AWS_REGION"},
		{"same shared domains", func(c *EmailConfig) { c.SharedTrustedDomain = c.SharedProbationDomain }, "EMAIL_SHARED_TRUSTED_DOMAIN"},
		{"nested shared domains", func(c *EmailConfig) { c.SharedTrustedDomain = "x." + c.SharedProbationDomain }, "EMAIL_SHARED_TRUSTED_DOMAIN"},
		{"invalid probation domain", func(c *EmailConfig) { c.SharedProbationDomain = "nodot" }, "EMAIL_SHARED_PROBATION_DOMAIN"},
		{"public base not absolute", func(c *EmailConfig) { c.PublicBaseURL = "/api" }, "API_BASE_URL"},
		{"public base http", func(c *EmailConfig) { c.PublicBaseURL = "http://api.example.com" }, "API_BASE_URL"},
		{"endpoint override not url", func(c *EmailConfig) { c.AWSEndpointURL = "nota url" }, "EMAIL_AWS_ENDPOINT_URL"},
		{"probation zone not a hosted zone id", func(c *EmailConfig) { c.Route53ProbationZoneID = "e903cf084b3b9aa43f2dec5f3b02b992" }, "EMAIL_ROUTE53_PROBATION_ZONE_ID"},
		{"trusted zone with a path", func(c *EmailConfig) { c.Route53TrustedZoneID = "/hostedzone/Z06895732Z3BD5VGPWD6P" }, "EMAIL_ROUTE53_TRUSTED_ZONE_ID"},
		{"one zone for both shared domains", func(c *EmailConfig) { c.Route53TrustedZoneID = c.Route53ProbationZoneID }, "EMAIL_ROUTE53_TRUSTED_ZONE_ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validEmailConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %s", err, tc.want)
			}
		})
	}
}

func TestLoadEmailConfigReadsEveryVariable(t *testing.T) {
	cfg := validEmailConfig()
	env := map[string]string{
		"EMAIL_AWS_REGION":                cfg.AWSRegion,
		"EMAIL_AWS_ACCOUNT_ID":            cfg.AWSAccountID,
		"EMAIL_AWS_ACCESS_KEY_ID":         cfg.AWSAccessKeyID,
		"EMAIL_AWS_SECRET_ACCESS_KEY":     cfg.AWSSecretAccessKey,
		"EMAIL_SNS_EVENTS_TOPIC_ARN":      cfg.EventsTopicARN,
		"EMAIL_SNS_INBOUND_TOPIC_ARN":     cfg.InboundTopicARN,
		"EMAIL_SNS_ALLOWED_TOPIC_ARNS":    strings.Join(cfg.AllowedTopicARNs, " , "),
		"EMAIL_INBOUND_BUCKET":            cfg.InboundBucket,
		"EMAIL_SHARED_PROBATION_DOMAIN":   "Envios.Example.com.br.",
		"EMAIL_SHARED_TRUSTED_DOMAIN":     cfg.SharedTrustedDomain,
		"EMAIL_ROUTE53_PROBATION_ZONE_ID": " " + cfg.Route53ProbationZoneID + " ",
		"EMAIL_ROUTE53_TRUSTED_ZONE_ID":   cfg.Route53TrustedZoneID,
		"EMAIL_TOKEN_SECRET":              cfg.TokenSecret,
		"GOOGLE_POSTMASTER_CLIENT_ID":     cfg.PostmasterClientID,
		"GOOGLE_POSTMASTER_CLIENT_SECRET": cfg.PostmasterClientSecret,
		"GOOGLE_POSTMASTER_REFRESH_TOKEN": cfg.PostmasterRefreshToken,
		"API_BASE_URL":                    cfg.PublicBaseURL + "/",
		"EMAIL_AWS_ENDPOINT_URL":          "https://ses.test/",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	got, missing := readEmailConfig()
	if len(missing) != 0 {
		t.Fatalf("missing = %v", missing)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got.Route53ProbationZoneID != cfg.Route53ProbationZoneID || got.Route53TrustedZoneID != cfg.Route53TrustedZoneID {
		t.Fatalf("zones: %+v", got)
	}
	if got.SharedProbationDomain != "envios.example.com.br" || got.PublicBaseURL != "https://api.example.com" || got.AWSEndpointURL != "https://ses.test" {
		t.Fatalf("normalization: %+v", got)
	}
	if len(got.AllowedTopicARNs) != 2 {
		t.Fatalf("allowed = %v", got.AllowedTopicARNs)
	}

	for _, retired := range []string{"ZEROBOUNCE_API_KEY", "ZEROBOUNCE_API_BASE_URL", "ENTRI_APPLICATION_ID", "ENTRI_SECRET", "ENTRI_API_BASE_URL", "EMAIL_CLOUDFLARE_API_TOKEN", "EMAIL_CLOUDFLARE_PROBATION_ZONE_ID", "EMAIL_CLOUDFLARE_TRUSTED_ZONE_ID"} {
		t.Setenv(retired, "")
	}
	if _, missing = readEmailConfig(); len(missing) != 0 {
		t.Fatalf("boot must not need the retired list validation or dns assistant keys, missing = %v", missing)
	}

	t.Setenv("EMAIL_ROUTE53_TRUSTED_ZONE_ID", "")
	t.Setenv("EMAIL_TOKEN_SECRET", "")
	t.Setenv("GOOGLE_POSTMASTER_REFRESH_TOKEN", " ")
	_, missing = readEmailConfig()
	if strings.Join(missing, ",") != "EMAIL_ROUTE53_TRUSTED_ZONE_ID,EMAIL_TOKEN_SECRET,GOOGLE_POSTMASTER_REFRESH_TOKEN" {
		t.Fatalf("missing = %v", missing)
	}
}
