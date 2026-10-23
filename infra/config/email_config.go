package config

import (
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"

	"vozko/domain/emailaddress"
)

const minEmailTokenSecretLength = 32

var (
	awsRegionPattern    = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)
	awsAccountIDPattern = regexp.MustCompile(`^\d{12}$`)
	hostedZoneIDPattern = regexp.MustCompile(`^Z[A-Z0-9]{1,31}$`)
)

type EmailConfig struct {
	AWSRegion          string
	AWSAccountID       string
	AWSAccessKeyID     string
	AWSSecretAccessKey string
	AWSEndpointURL     string

	EventsTopicARN   string
	InboundTopicARN  string
	AllowedTopicARNs []string
	InboundBucket    string

	SharedProbationDomain string
	SharedTrustedDomain   string

	Route53ProbationZoneID string
	Route53TrustedZoneID   string

	TokenSecret string

	PostmasterRefreshToken string

	PublicBaseURL string
}

func loadEmailConfig() EmailConfig {
	cfg, missing := readEmailConfig()
	if len(missing) > 0 {
		log.Fatalf("required env var %s is not set", missing[0])
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid email configuration: %v", err)
	}
	return cfg
}

func readEmailConfig() (EmailConfig, []string) {
	var missing []string
	required := func(key string) string {
		v := strings.TrimSpace(trimEnv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	cfg := EmailConfig{
		AWSRegion:              required("EMAIL_AWS_REGION"),
		AWSAccountID:           required("EMAIL_AWS_ACCOUNT_ID"),
		AWSAccessKeyID:         required("EMAIL_AWS_ACCESS_KEY_ID"),
		AWSSecretAccessKey:     required("EMAIL_AWS_SECRET_ACCESS_KEY"),
		AWSEndpointURL:         strings.TrimRight(trimEnv("EMAIL_AWS_ENDPOINT_URL"), "/"),
		EventsTopicARN:         required("EMAIL_SNS_EVENTS_TOPIC_ARN"),
		InboundTopicARN:        required("EMAIL_SNS_INBOUND_TOPIC_ARN"),
		AllowedTopicARNs:       splitTrimmed(required("EMAIL_SNS_ALLOWED_TOPIC_ARNS")),
		InboundBucket:          required("EMAIL_INBOUND_BUCKET"),
		SharedProbationDomain:  normalizedDomain(required("EMAIL_SHARED_PROBATION_DOMAIN")),
		SharedTrustedDomain:    normalizedDomain(required("EMAIL_SHARED_TRUSTED_DOMAIN")),
		Route53ProbationZoneID: required("EMAIL_ROUTE53_PROBATION_ZONE_ID"),
		Route53TrustedZoneID:   required("EMAIL_ROUTE53_TRUSTED_ZONE_ID"),
		TokenSecret:            required("EMAIL_TOKEN_SECRET"),
		PostmasterRefreshToken: required("GOOGLE_POSTMASTER_REFRESH_TOKEN"),
		PublicBaseURL:          strings.TrimRight(required("API_BASE_URL"), "/"),
	}
	return cfg, missing
}

func normalizedDomain(raw string) string {
	if d, err := emailaddress.NormalizeDomain(raw); err == nil {
		return d
	}
	return raw
}

func (c EmailConfig) Validate() error {
	if !awsRegionPattern.MatchString(c.AWSRegion) {
		return fmt.Errorf("EMAIL_AWS_REGION %q is not an AWS region", c.AWSRegion)
	}
	if !awsAccountIDPattern.MatchString(c.AWSAccountID) {
		return fmt.Errorf("EMAIL_AWS_ACCOUNT_ID must have 12 digits")
	}
	if len(c.TokenSecret) < minEmailTokenSecretLength {
		return fmt.Errorf("EMAIL_TOKEN_SECRET must have at least %d characters", minEmailTokenSecretLength)
	}
	for _, arn := range c.AllowedTopicARNs {
		if !c.snsTopicInRegion(arn) {
			return fmt.Errorf("EMAIL_SNS_ALLOWED_TOPIC_ARNS has %q, which is not an SNS topic of account %s in %s", arn, c.AWSAccountID, c.AWSRegion)
		}
	}
	if !c.topicAllowed(c.EventsTopicARN) {
		return fmt.Errorf("EMAIL_SNS_EVENTS_TOPIC_ARN must be listed in EMAIL_SNS_ALLOWED_TOPIC_ARNS")
	}
	if !c.topicAllowed(c.InboundTopicARN) {
		return fmt.Errorf("EMAIL_SNS_INBOUND_TOPIC_ARN must be listed in EMAIL_SNS_ALLOWED_TOPIC_ARNS")
	}
	if _, err := emailaddress.NormalizeDomain(c.SharedProbationDomain); err != nil {
		return fmt.Errorf("EMAIL_SHARED_PROBATION_DOMAIN %q is not a domain", c.SharedProbationDomain)
	}
	if _, err := emailaddress.NormalizeDomain(c.SharedTrustedDomain); err != nil {
		return fmt.Errorf("EMAIL_SHARED_TRUSTED_DOMAIN %q is not a domain", c.SharedTrustedDomain)
	}
	if nestedDomains(c.SharedProbationDomain, c.SharedTrustedDomain) {
		return fmt.Errorf("EMAIL_SHARED_TRUSTED_DOMAIN and EMAIL_SHARED_PROBATION_DOMAIN must be separate domains")
	}
	if !hostedZoneIDPattern.MatchString(c.Route53ProbationZoneID) {
		return fmt.Errorf("EMAIL_ROUTE53_PROBATION_ZONE_ID %q is not a Route 53 hosted zone id", c.Route53ProbationZoneID)
	}
	if !hostedZoneIDPattern.MatchString(c.Route53TrustedZoneID) {
		return fmt.Errorf("EMAIL_ROUTE53_TRUSTED_ZONE_ID %q is not a Route 53 hosted zone id", c.Route53TrustedZoneID)
	}
	if c.Route53TrustedZoneID == c.Route53ProbationZoneID {
		return fmt.Errorf("EMAIL_ROUTE53_TRUSTED_ZONE_ID and EMAIL_ROUTE53_PROBATION_ZONE_ID must be separate hosted zones")
	}
	if !absoluteURL(c.PublicBaseURL, true) {
		return fmt.Errorf("API_BASE_URL must be an absolute https url for the email unsubscribe links")
	}
	if c.AWSEndpointURL != "" && !absoluteURL(c.AWSEndpointURL, false) {
		return fmt.Errorf("EMAIL_AWS_ENDPOINT_URL must be an absolute url when set")
	}
	return nil
}

func (c EmailConfig) topicAllowed(arn string) bool {
	for _, allowed := range c.AllowedTopicARNs {
		if allowed == arn {
			return true
		}
	}
	return false
}

func (c EmailConfig) snsTopicInRegion(arn string) bool {
	parts := strings.Split(arn, ":")
	return len(parts) == 6 && parts[0] == "arn" && parts[1] == "aws" && parts[2] == "sns" &&
		parts[3] == c.AWSRegion && parts[4] == c.AWSAccountID && parts[5] != ""
}

func nestedDomains(a, b string) bool {
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

func absoluteURL(raw string, httpsOnly bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if httpsOnly {
		return u.Scheme == "https"
	}
	return u.Scheme == "https" || u.Scheme == "http"
}
