package email_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gomail "github.com/wneessen/go-mail"
)

type fakeGoMailClient struct {
	calls    int
	messages int
	err      error
}

func (c *fakeGoMailClient) DialAndSendWithContext(_ context.Context, messages ...*gomail.Msg) error {
	c.calls++
	c.messages = len(messages)
	return c.err
}

func validSMTPConfigMap() map[string]interface{} {
	return map[string]interface{}{
		"smtp_host":       "smtp.example.com",
		"smtp_from_email": "sender@example.com",
		"smtp_security":   "starttls",
	}
}

func validEmailParams() map[string]interface{} {
	return map[string]interface{}{
		"to":      "customer@example.com",
		"subject": "Hello",
		"body":    "<p>Hello</p>",
	}
}

func TestSMTPConfigFromMap_DefaultsAndAliases(t *testing.T) {
	cfg, err := SMTPConfigFromMap(validSMTPConfigMap())
	if err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if cfg.Port != 587 || cfg.Security != SMTPSecurityStartTLS || cfg.Timeout != DefaultSMTPTimeout {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	implicit := validSMTPConfigMap()
	implicit["smtp_security"] = "ssl"
	cfg, err = SMTPConfigFromMap(implicit)
	if err != nil || cfg.Security != SMTPSecurityImplicitTLS || cfg.Port != 465 {
		t.Fatalf("expected implicit TLS defaults, got cfg=%+v err=%v", cfg, err)
	}

	plain := validSMTPConfigMap()
	plain["smtp_security"] = "plain"
	cfg, err = SMTPConfigFromMap(plain)
	if err != nil || cfg.Security != SMTPSecurityNone || cfg.Port != 25 {
		t.Fatalf("expected no TLS defaults, got cfg=%+v err=%v", cfg, err)
	}
}

func TestSMTPConfigFromMap_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]interface{})
		want   string
	}{
		{"missing host", func(m map[string]interface{}) { delete(m, "smtp_host") }, "smtp_host is required"},
		{"host url", func(m map[string]interface{}) { m["smtp_host"] = "smtp://example.com" }, "not a URL"},
		{"host crlf", func(m map[string]interface{}) { m["smtp_host"] = "smtp.example.com\r\nX" }, "line breaks"},
		{"bad port type", func(m map[string]interface{}) { m["smtp_port"] = true }, "must be a number"},
		{"bad port string", func(m map[string]interface{}) { m["smtp_port"] = "abc" }, "must be a number"},
		{"port out of range", func(m map[string]interface{}) { m["smtp_port"] = 70000 }, "between 1 and 65535"},
		{"bad security", func(m map[string]interface{}) { m["smtp_security"] = "required_tls" }, "smtp_security"},
		{"bad timeout", func(m map[string]interface{}) { m["timeout_seconds"] = 121 }, "timeout_seconds"},
		{"bad timeout string", func(m map[string]interface{}) { m["timeout_seconds"] = "slow" }, "must be a number"},
		{"missing from", func(m map[string]interface{}) { delete(m, "smtp_from_email") }, "smtp_from_email is required"},
		{"bad from", func(m map[string]interface{}) { m["smtp_from_email"] = "not-email" }, "valid email"},
		{"from name crlf", func(m map[string]interface{}) { m["smtp_from_name"] = "ACME\nBcc" }, "smtp_from_name"},
		{"username without password", func(m map[string]interface{}) { m["smtp_username"] = "user" }, "provided together"},
		{"password without username", func(m map[string]interface{}) { m["smtp_password"] = "secret" }, "provided together"},
		{"credentials crlf", func(m map[string]interface{}) { m["smtp_username"] = "user\nname"; m["smtp_password"] = "secret" }, "credentials"},
		{"auth without tls", func(m map[string]interface{}) {
			m["smtp_security"] = "none"
			m["smtp_username"] = "user"
			m["smtp_password"] = "secret"
		}, "requires starttls"},
		{"bad reply", func(m map[string]interface{}) { m["default_reply_to"] = "bad" }, "default_reply_to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validSMTPConfigMap()
			tt.change(input)
			_, err := SMTPConfigFromMap(input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestEmailMessageFromMap_ParsesRecipientsAndDefaults(t *testing.T) {
	params := validEmailParams()
	params["to"] = "Ana <ana@example.com>, bob@example.com"
	params["cc"] = []interface{}{"cc1@example.com", "CC Two <cc2@example.com>"}
	params["bcc"] = []string{"hidden@example.com"}
	params["reply_to"] = "reply@example.com"
	params["body_type"] = "text/plain"

	message, err := EmailMessageFromMap(params)
	if err != nil {
		t.Fatalf("expected valid message, got %v", err)
	}
	if len(message.To) != 2 || message.To[0] != "ana@example.com" || message.To[1] != "bob@example.com" {
		t.Fatalf("unexpected to recipients: %+v", message.To)
	}
	if len(message.Cc) != 2 || message.Cc[1] != "cc2@example.com" || len(message.Bcc) != 1 {
		t.Fatalf("unexpected cc/bcc recipients: cc=%+v bcc=%+v", message.Cc, message.Bcc)
	}
	if message.BodyType != EmailBodyTypeText || message.ReplyTo != "reply@example.com" {
		t.Fatalf("unexpected message metadata: %+v", message)
	}
}

func TestEmailMessageFromMap_RejectsInvalidMessage(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]interface{})
		want   string
	}{
		{"missing recipients", func(m map[string]interface{}) { delete(m, "to") }, "recipient"},
		{"invalid to", func(m map[string]interface{}) { m["to"] = "bad" }, "invalid email"},
		{"to crlf", func(m map[string]interface{}) { m["to"] = "a@example.com\nBcc: b@example.com" }, "line breaks"},
		{"invalid cc type", func(m map[string]interface{}) { m["cc"] = 4 }, "string or array"},
		{"invalid cc item", func(m map[string]interface{}) { m["cc"] = []interface{}{4} }, "must be strings"},
		{"invalid bcc", func(m map[string]interface{}) { m["bcc"] = "bad" }, "invalid email"},
		{"missing subject", func(m map[string]interface{}) { m["subject"] = " " }, "subject is required"},
		{"subject crlf", func(m map[string]interface{}) { m["subject"] = "Hello\nBcc" }, "line breaks"},
		{"subject too long", func(m map[string]interface{}) { m["subject"] = strings.Repeat("a", MaxEmailSubjectRunes+1) }, "at most"},
		{"missing body", func(m map[string]interface{}) { m["body"] = " " }, "body is required"},
		{"body too large", func(m map[string]interface{}) { m["body"] = strings.Repeat("a", MaxEmailBodyBytes+1) }, "at most"},
		{"bad body type", func(m map[string]interface{}) { m["body_type"] = "markdown" }, "body_type"},
		{"bad reply", func(m map[string]interface{}) { m["reply_to"] = "bad" }, "reply_to"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := validEmailParams()
			tt.change(params)
			_, err := EmailMessageFromMap(params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestEmailMessageValidate_RecipientBranches(t *testing.T) {
	tests := []struct {
		name    string
		message EmailMessage
		want    string
	}{
		{"invalid to", EmailMessage{To: []string{"bad"}, Subject: "Hi", Body: "Body", BodyType: EmailBodyTypeText}, "to"},
		{"invalid cc", EmailMessage{To: []string{"to@example.com"}, Cc: []string{"bad"}, Subject: "Hi", Body: "Body", BodyType: EmailBodyTypeText}, "cc"},
		{"invalid bcc", EmailMessage{To: []string{"to@example.com"}, Bcc: []string{"bad"}, Subject: "Hi", Body: "Body", BodyType: EmailBodyTypeText}, "bcc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.message.Normalized(SMTPConfig{}).Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRecipientsFromValue_AdditionalBranches(t *testing.T) {
	if got, err := RecipientsFromValue("to", " "); err != nil || len(got) != 0 {
		t.Fatalf("expected empty string to produce no recipients, got %v err=%v", got, err)
	}
	if _, err := RecipientsFromValue("to", []string{"good@example.com", "bad"}); err == nil {
		t.Fatal("expected invalid []string recipient to fail")
	}
	if _, err := RecipientsFromValue("to", []interface{}{"good@example.com", "bad"}); err == nil {
		t.Fatal("expected invalid []interface{} recipient to fail")
	}
	if _, err := RecipientsFromValue("to", "<>"); err == nil {
		t.Fatal("expected empty parsed address to fail")
	}
}

func TestGoMailSMTPSender_SendUsesFactoryAndClient(t *testing.T) {
	client := &fakeGoMailClient{}
	sender := &GoMailSMTPSender{
		NewClient: func(host string, opts ...gomail.Option) (goMailClient, error) {
			if host != "smtp.example.com" {
				t.Fatalf("unexpected host %q", host)
			}
			if len(opts) == 0 {
				t.Fatal("expected mail client options")
			}
			return client, nil
		},
	}

	result, err := sender.Send(nil, SMTPConfig{
		Host:      "smtp.example.com",
		FromEmail: "sender@example.com",
		FromName:  "ACME",
		Security:  SMTPSecurityStartTLS,
	}, EmailMessage{
		To:      []string{"customer@example.com"},
		Subject: "Hello",
		Body:    "<p>Hello</p>",
	})
	if err != nil {
		t.Fatalf("expected send success, got %v", err)
	}
	if client.calls != 1 || client.messages != 1 {
		t.Fatalf("expected one message send, got calls=%d messages=%d", client.calls, client.messages)
	}
	if result.MessageID == "" || result.To[0] != "customer@example.com" || result.Subject != "Hello" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestGoMailSMTPSender_ConstructorsAndValidationErrors(t *testing.T) {
	if NewGoMailSMTPSender() == nil {
		t.Fatal("expected constructor to return sender")
	}
	if _, err := defaultGoMailClientFactory("smtp.example.com"); err != nil {
		t.Fatalf("expected default factory to create a client, got %v", err)
	}

	sender := &GoMailSMTPSender{NewClient: func(string, ...gomail.Option) (goMailClient, error) {
		t.Fatal("factory should not be called for invalid input")
		return nil, nil
	}}
	_, err := sender.Send(context.Background(), SMTPConfig{}, EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body"})
	if err == nil || !strings.Contains(err.Error(), "smtp_host") {
		t.Fatalf("expected config validation error, got %v", err)
	}
	_, err = sender.Send(context.Background(), SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com"}, EmailMessage{Subject: "Hi", Body: "Body"})
	if err == nil || !strings.Contains(err.Error(), "recipient") {
		t.Fatalf("expected message validation error, got %v", err)
	}

	buildErr := errors.New("build failed")
	sender = &GoMailSMTPSender{
		buildMessage: func(SMTPConfig, EmailMessage) (*gomail.Msg, error) { return nil, buildErr },
		NewClient: func(string, ...gomail.Option) (goMailClient, error) {
			t.Fatal("factory should not be called when message build fails")
			return nil, nil
		},
	}
	_, err = sender.Send(context.Background(), SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com"}, EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body"})
	if !errors.Is(err, buildErr) {
		t.Fatalf("expected build error, got %v", err)
	}
}

func TestGoMailSMTPSender_SendCoversSecurityModesAndErrors(t *testing.T) {
	for _, security := range []SMTPSecurity{SMTPSecurityStartTLS, SMTPSecurityImplicitTLS, SMTPSecurityNone} {
		t.Run(string(security), func(t *testing.T) {
			client := &fakeGoMailClient{}
			sender := &GoMailSMTPSender{NewClient: func(string, ...gomail.Option) (goMailClient, error) { return client, nil }}
			_, err := sender.Send(context.Background(), SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com", Security: security}, EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body"})
			if err != nil {
				t.Fatalf("expected security mode %s to send, got %v", security, err)
			}
		})
	}

	senderWithAuth := &GoMailSMTPSender{NewClient: func(string, ...gomail.Option) (goMailClient, error) { return &fakeGoMailClient{}, nil }}
	_, err := senderWithAuth.Send(context.Background(), SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com", Username: "user", Password: "secret"}, EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body"})
	if err != nil {
		t.Fatalf("expected authenticated STARTTLS config to send, got %v", err)
	}

	factoryErr := errors.New("factory failed")
	sender := &GoMailSMTPSender{NewClient: func(string, ...gomail.Option) (goMailClient, error) { return nil, factoryErr }}
	_, err = sender.Send(context.Background(), SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com"}, EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body"})
	if !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}

	clientErr := errors.New("smtp failed")
	sender = &GoMailSMTPSender{NewClient: func(string, ...gomail.Option) (goMailClient, error) { return &fakeGoMailClient{err: clientErr}, nil }}
	_, err = sender.Send(context.Background(), SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com", Timeout: time.Second}, EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body"})
	if !errors.Is(err, clientErr) {
		t.Fatalf("expected client error, got %v", err)
	}
}

func TestBuildGoMailMessage_ErrorBranchesAndTextBody(t *testing.T) {
	cfg := SMTPConfig{Host: "smtp.example.com", FromEmail: "sender@example.com"}.Normalized()
	message := EmailMessage{To: []string{"to@example.com"}, Subject: "Hi", Body: "Body", BodyType: EmailBodyTypeText}.Normalized(cfg)
	if _, err := buildGoMailMessage(cfg, message); err != nil {
		t.Fatalf("expected text message to build, got %v", err)
	}

	tests := []struct {
		name    string
		config  SMTPConfig
		message EmailMessage
		want    string
	}{
		{"bad from", SMTPConfig{FromEmail: "bad"}, message, "from"},
		{"bad to", cfg, EmailMessage{To: []string{"bad"}, Subject: "Hi", Body: "Body"}, "to"},
		{"bad cc", cfg, EmailMessage{To: []string{"to@example.com"}, Cc: []string{"bad"}, Subject: "Hi", Body: "Body"}, "cc"},
		{"bad bcc", cfg, EmailMessage{To: []string{"to@example.com"}, Bcc: []string{"bad"}, Subject: "Hi", Body: "Body"}, "bcc"},
		{"bad reply", cfg, EmailMessage{To: []string{"to@example.com"}, ReplyTo: "bad", Subject: "Hi", Body: "Body"}, "reply_to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildGoMailMessage(tt.config, tt.message.Normalized(tt.config))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected build error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestScalarHelpers(t *testing.T) {
	if got, err := optionalInt(nil, "missing"); err != nil || got != 0 {
		t.Fatalf("expected nil map int default, got %d err=%v", got, err)
	}
	if got, err := optionalInt(map[string]interface{}{"v": int64(7)}, "v"); err != nil || got != 7 {
		t.Fatalf("expected int64 parse, got %d err=%v", got, err)
	}
	if got, err := optionalInt(map[string]interface{}{"v": float64(8)}, "v"); err != nil || got != 8 {
		t.Fatalf("expected float64 parse, got %d err=%v", got, err)
	}
	if got, err := optionalInt(map[string]interface{}{"v": float32(9)}, "v"); err != nil || got != 9 {
		t.Fatalf("expected float32 parse, got %d err=%v", got, err)
	}
	if got, err := optionalInt(map[string]interface{}{"v": " "}, "v"); err != nil || got != 0 {
		t.Fatalf("expected blank string default, got %d err=%v", got, err)
	}
	if got, err := optionalInt(map[string]interface{}{"v": "10"}, "v"); err != nil || got != 10 {
		t.Fatalf("expected numeric string parse, got %d err=%v", got, err)
	}
	if got := stringValue(nil, "missing"); got != "" {
		t.Fatalf("expected empty string from nil map, got %q", got)
	}
	if got := stringValue(map[string]interface{}{"v": 42}, "v"); got != "42" {
		t.Fatalf("expected formatted non-string value, got %q", got)
	}
	if err := validateSingleAddress("email", " "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected empty address error, got %v", err)
	}
	if err := validateSingleAddress("email", "a@example.com\nBcc: b@example.com"); err == nil || !strings.Contains(err.Error(), "line breaks") {
		t.Fatalf("expected line break address error, got %v", err)
	}
	if err := validateSingleAddress("email", "<>"); err == nil || !strings.Contains(err.Error(), "valid email") {
		t.Fatalf("expected empty parsed address error, got %v", err)
	}
}
