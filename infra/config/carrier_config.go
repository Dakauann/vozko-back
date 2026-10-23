package config

import (
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"

	"vozko/domain/shared"
)

const minCarrierLinkSecretLength = 32

var (
	carrierWebhookTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)
	smsNumberPattern           = regexp.MustCompile(`^[1-9][0-9]{2,15}$`)
)

type CarrierConfig struct {
	CMComProductToken string
	CMComAccountID    string
	CMComGatewayURL   string
	CMComAPIURL       string
	WebhookToken      string
	LinkSecret        string
	SMSSharedNumber   string
	PublicBaseURL     string
}

func loadCarrierConfig() CarrierConfig {
	cfg, missing := readCarrierConfig()
	if len(missing) > 0 {
		log.Fatalf("required env var %s is not set", missing[0])
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid RCS and SMS configuration: %v", err)
	}
	return cfg
}

func readCarrierConfig() (CarrierConfig, []string) {
	var missing []string
	required := func(key string) string {
		v := strings.TrimSpace(trimEnv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	cfg := CarrierConfig{
		CMComProductToken: required("CARRIER_CMCOM_PRODUCT_TOKEN"),
		CMComAccountID:    required("CARRIER_CMCOM_ACCOUNT_ID"),
		CMComGatewayURL:   strings.TrimRight(trimEnv("CARRIER_CMCOM_GATEWAY_URL"), "/"),
		CMComAPIURL:       strings.TrimRight(trimEnv("CARRIER_CMCOM_API_URL"), "/"),
		WebhookToken:      required("CARRIER_WEBHOOK_TOKEN"),
		LinkSecret:        required("CARRIER_LINK_SECRET"),
		SMSSharedNumber:   required("SMS_SHARED_NUMBER"),
		PublicBaseURL:     strings.TrimRight(required("API_BASE_URL"), "/"),
	}
	return cfg, missing
}

func (c CarrierConfig) Validate() error {
	if !carrierWebhookTokenPattern.MatchString(c.WebhookToken) {
		return fmt.Errorf("CARRIER_WEBHOOK_TOKEN must have 43 to 128 base64url characters")
	}
	if len(c.LinkSecret) < minCarrierLinkSecretLength {
		return fmt.Errorf("CARRIER_LINK_SECRET must have at least %d characters", minCarrierLinkSecretLength)
	}
	if !smsNumberPattern.MatchString(c.SMSSharedNumber) {
		return fmt.Errorf("SMS_SHARED_NUMBER must be the short code or number in digits")
	}
	u, err := url.Parse(c.PublicBaseURL)
	if err != nil || u.Hostname() == "" || !(u.Scheme == "https" || (u.Scheme == "http" && shared.IsLoopbackHost(u.Hostname()))) {
		return fmt.Errorf("API_BASE_URL must be the public https address of the API, or http on localhost for development")
	}
	return nil
}
