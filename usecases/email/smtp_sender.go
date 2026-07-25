package email_usecase

import (
	"context"
	"crypto/tls"
	"fmt"
	netmail "net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	gomail "github.com/wneessen/go-mail"
)

const (
	MaxEmailBodyBytes    = 100 * 1024
	MaxEmailSubjectRunes = 200
	DefaultSMTPTimeout   = 30 * time.Second
	MaxSMTPTimeout       = 120 * time.Second
)

type SMTPSecurity string

const (
	SMTPSecurityStartTLS    SMTPSecurity = "starttls"
	SMTPSecurityImplicitTLS SMTPSecurity = "implicit_tls"
	SMTPSecurityNone        SMTPSecurity = "none"
)

type EmailBodyType string

const (
	EmailBodyTypeHTML EmailBodyType = "html"
	EmailBodyTypeText EmailBodyType = "text"
)

type SMTPConfig struct {
	Host           string
	Port           int
	Username       string
	Password       string
	FromEmail      string
	FromName       string
	DefaultReplyTo string
	Security       SMTPSecurity
	Timeout        time.Duration
}

type EmailMessage struct {
	To       []string
	Cc       []string
	Bcc      []string
	Subject  string
	Body     string
	BodyType EmailBodyType
	ReplyTo  string
}

type SendResult struct {
	To             []string
	Cc             []string
	Bcc            []string
	Subject        string
	MessageID      string
	ServerResponse string
}

type SMTPSender interface {
	Send(ctx context.Context, config SMTPConfig, message EmailMessage) (*SendResult, error)
}

type goMailClient interface {
	DialAndSendWithContext(ctx context.Context, messages ...*gomail.Msg) error
}

type GoMailSMTPSender struct {
	NewClient    func(host string, opts ...gomail.Option) (goMailClient, error)
	buildMessage func(config SMTPConfig, message EmailMessage) (*gomail.Msg, error)
}

func NewGoMailSMTPSender() *GoMailSMTPSender {
	return &GoMailSMTPSender{}
}

func (s *GoMailSMTPSender) Send(ctx context.Context, config SMTPConfig, message EmailMessage) (*SendResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	config = config.Normalized()
	message = message.Normalized(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := message.Validate(); err != nil {
		return nil, err
	}

	buildMessage := buildGoMailMessage
	if s != nil && s.buildMessage != nil {
		buildMessage = s.buildMessage
	}
	mailMessage, err := buildMessage(config, message)
	if err != nil {
		return nil, err
	}

	factory := defaultGoMailClientFactory
	if s != nil && s.NewClient != nil {
		factory = s.NewClient
	}
	client, err := factory(config.Host, goMailOptions(config)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SMTP client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	if err := client.DialAndSendWithContext(ctx, mailMessage); err != nil {
		return nil, fmt.Errorf("failed to send email via SMTP: %w", err)
	}

	return &SendResult{
		To:             append([]string(nil), message.To...),
		Cc:             append([]string(nil), message.Cc...),
		Bcc:            append([]string(nil), message.Bcc...),
		Subject:        message.Subject,
		MessageID:      mailMessage.GetMessageID(),
		ServerResponse: mailMessage.ServerResponse(),
	}, nil
}

func defaultGoMailClientFactory(host string, opts ...gomail.Option) (goMailClient, error) {
	return gomail.NewClient(host, opts...)
}

func (c SMTPConfig) Normalized() SMTPConfig {
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	c.FromEmail = strings.TrimSpace(c.FromEmail)
	c.FromName = strings.TrimSpace(c.FromName)
	c.DefaultReplyTo = strings.TrimSpace(c.DefaultReplyTo)
	c.Security = NormalizeSMTPSecurity(string(c.Security))
	if c.Port == 0 {
		switch c.Security {
		case SMTPSecurityImplicitTLS:
			c.Port = 465
		case SMTPSecurityNone:
			c.Port = 25
		default:
			c.Port = 587
		}
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultSMTPTimeout
	}
	return c
}

func (c SMTPConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("smtp_host is required")
	}
	if hasCRLF(c.Host) {
		return fmt.Errorf("smtp_host contains invalid line breaks")
	}
	if strings.Contains(c.Host, "://") {
		return fmt.Errorf("smtp_host must be a host name, not a URL")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("smtp_port must be between 1 and 65535")
	}
	if !c.Security.Valid() {
		return fmt.Errorf("smtp_security must be one of starttls, implicit_tls, none")
	}
	if c.Timeout <= 0 || c.Timeout > MaxSMTPTimeout {
		return fmt.Errorf("timeout_seconds must be greater than 0 and at most %d", int(MaxSMTPTimeout/time.Second))
	}
	if c.FromEmail == "" {
		return fmt.Errorf("smtp_from_email is required")
	}
	if err := validateSingleAddress("smtp_from_email", c.FromEmail); err != nil {
		return err
	}
	if hasCRLF(c.FromName) {
		return fmt.Errorf("smtp_from_name contains invalid line breaks")
	}
	if hasCRLF(c.Username) || hasCRLF(c.Password) {
		return fmt.Errorf("SMTP credentials contain invalid line breaks")
	}
	if (c.Username == "") != (c.Password == "") {
		return fmt.Errorf("smtp_username and smtp_password must be provided together")
	}
	if c.Security == SMTPSecurityNone && c.Username != "" {
		return fmt.Errorf("SMTP authentication requires starttls or implicit_tls")
	}
	if c.DefaultReplyTo != "" {
		if err := validateSingleAddress("default_reply_to", c.DefaultReplyTo); err != nil {
			return err
		}
	}
	return nil
}

func (s SMTPSecurity) Valid() bool {
	switch s {
	case SMTPSecurityStartTLS, SMTPSecurityImplicitTLS, SMTPSecurityNone:
		return true
	default:
		return false
	}
}

func NormalizeSMTPSecurity(value string) SMTPSecurity {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "starttls", "start_tls", "start-tls":
		return SMTPSecurityStartTLS
	case "implicit_tls", "implicit-tls", "ssl", "tls", "smtps":
		return SMTPSecurityImplicitTLS
	case "none", "plain", "no_tls", "notls":
		return SMTPSecurityNone
	default:
		return SMTPSecurity(value)
	}
}

func (m EmailMessage) Normalized(config SMTPConfig) EmailMessage {
	m.To = normalizeRecipientSlice(m.To)
	m.Cc = normalizeRecipientSlice(m.Cc)
	m.Bcc = normalizeRecipientSlice(m.Bcc)
	m.Subject = strings.TrimSpace(m.Subject)
	m.Body = strings.TrimSpace(m.Body)
	m.BodyType = NormalizeEmailBodyType(string(m.BodyType))
	m.ReplyTo = strings.TrimSpace(m.ReplyTo)
	if m.ReplyTo == "" {
		m.ReplyTo = config.DefaultReplyTo
	}
	return m
}

func (m EmailMessage) Validate() error {
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		return fmt.Errorf("at least one email recipient is required")
	}
	if err := validateAddressList("to", m.To); err != nil {
		return err
	}
	if err := validateAddressList("cc", m.Cc); err != nil {
		return err
	}
	if err := validateAddressList("bcc", m.Bcc); err != nil {
		return err
	}
	if m.Subject == "" {
		return fmt.Errorf("subject is required")
	}
	if hasCRLF(m.Subject) {
		return fmt.Errorf("subject contains invalid line breaks")
	}
	if utf8.RuneCountInString(m.Subject) > MaxEmailSubjectRunes {
		return fmt.Errorf("subject must be at most %d characters", MaxEmailSubjectRunes)
	}
	if m.Body == "" {
		return fmt.Errorf("body is required")
	}
	if len([]byte(m.Body)) > MaxEmailBodyBytes {
		return fmt.Errorf("body must be at most %d bytes", MaxEmailBodyBytes)
	}
	if !m.BodyType.Valid() {
		return fmt.Errorf("body_type must be one of html, text")
	}
	if m.ReplyTo != "" {
		if err := validateSingleAddress("reply_to", m.ReplyTo); err != nil {
			return err
		}
	}
	return nil
}

func (t EmailBodyType) Valid() bool {
	switch t {
	case EmailBodyTypeHTML, EmailBodyTypeText:
		return true
	default:
		return false
	}
}

func NormalizeEmailBodyType(value string) EmailBodyType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "html", "text/html":
		return EmailBodyTypeHTML
	case "text", "plain", "text/plain":
		return EmailBodyTypeText
	default:
		return EmailBodyType(value)
	}
}

func SMTPConfigFromMap(config map[string]interface{}) (SMTPConfig, error) {
	port, err := optionalInt(config, "smtp_port")
	if err != nil {
		return SMTPConfig{}, err
	}
	timeoutSeconds, err := optionalInt(config, "timeout_seconds")
	if err != nil {
		return SMTPConfig{}, err
	}
	cfg := SMTPConfig{
		Host:           stringValue(config, "smtp_host"),
		Port:           port,
		Username:       stringValue(config, "smtp_username"),
		Password:       stringValue(config, "smtp_password"),
		FromEmail:      stringValue(config, "smtp_from_email"),
		FromName:       stringValue(config, "smtp_from_name"),
		DefaultReplyTo: stringValue(config, "default_reply_to"),
		Security:       NormalizeSMTPSecurity(stringValue(config, "smtp_security")),
	}
	if timeoutSeconds > 0 {
		cfg.Timeout = time.Duration(timeoutSeconds) * time.Second
	}
	cfg = cfg.Normalized()
	return cfg, cfg.Validate()
}

func EmailMessageFromMap(params map[string]interface{}) (EmailMessage, error) {
	to, err := RecipientsFromValue("to", params["to"])
	if err != nil {
		return EmailMessage{}, err
	}
	cc, err := RecipientsFromValue("cc", params["cc"])
	if err != nil {
		return EmailMessage{}, err
	}
	bcc, err := RecipientsFromValue("bcc", params["bcc"])
	if err != nil {
		return EmailMessage{}, err
	}
	message := EmailMessage{
		To:       to,
		Cc:       cc,
		Bcc:      bcc,
		Subject:  stringValue(params, "subject"),
		Body:     stringValue(params, "body"),
		BodyType: NormalizeEmailBodyType(stringValue(params, "body_type")),
		ReplyTo:  stringValue(params, "reply_to"),
	}
	message = message.Normalized(SMTPConfig{})
	return message, message.Validate()
}

func RecipientsFromValue(field string, value interface{}) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	var recipients []string
	switch typed := value.(type) {
	case string:
		return parseRecipientString(field, typed)
	case []string:
		for _, item := range typed {
			parsed, err := parseRecipientString(field, item)
			if err != nil {
				return nil, err
			}
			recipients = append(recipients, parsed...)
		}
	case []interface{}:
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s recipients must be strings", field)
			}
			parsed, err := parseRecipientString(field, str)
			if err != nil {
				return nil, err
			}
			recipients = append(recipients, parsed...)
		}
	default:
		return nil, fmt.Errorf("%s recipients must be a string or array", field)
	}
	return normalizeRecipientSlice(recipients), nil
}

func parseRecipientString(field, raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if hasCRLF(raw) {
		return nil, fmt.Errorf("%s contains invalid line breaks", field)
	}
	addresses, err := netmail.ParseAddressList(raw)
	if err != nil {
		return nil, fmt.Errorf("%s contains invalid email address: %w", field, err)
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, strings.TrimSpace(address.Address))
	}
	return result, nil
}

func goMailOptions(config SMTPConfig) []gomail.Option {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}
	options := []gomail.Option{
		gomail.WithPort(config.Port),
		gomail.WithTimeout(config.Timeout),
		gomail.WithTLSConfig(tlsConfig),
	}
	switch config.Security {
	case SMTPSecurityImplicitTLS:
		options = append(options, gomail.WithSSL())
	case SMTPSecurityNone:
		options = append(options, gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		options = append(options, gomail.WithTLSPolicy(gomail.TLSMandatory))
	}
	if config.Username != "" {
		options = append(options,
			gomail.WithUsername(config.Username),
			gomail.WithPassword(config.Password),
			gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover),
		)
	}
	return options
}

func buildGoMailMessage(config SMTPConfig, message EmailMessage) (*gomail.Msg, error) {
	m := gomail.NewMsg()
	var err error
	if config.FromName != "" {
		err = m.FromFormat(config.FromName, config.FromEmail)
	} else {
		err = m.From(config.FromEmail)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}
	_ = m.EnvelopeFrom(config.FromEmail)
	if len(message.To) > 0 {
		if err := m.To(message.To...); err != nil {
			return nil, fmt.Errorf("invalid to address: %w", err)
		}
	}
	if len(message.Cc) > 0 {
		if err := m.Cc(message.Cc...); err != nil {
			return nil, fmt.Errorf("invalid cc address: %w", err)
		}
	}
	if len(message.Bcc) > 0 {
		if err := m.Bcc(message.Bcc...); err != nil {
			return nil, fmt.Errorf("invalid bcc address: %w", err)
		}
	}
	if message.ReplyTo != "" {
		if err := m.ReplyTo(message.ReplyTo); err != nil {
			return nil, fmt.Errorf("invalid reply_to address: %w", err)
		}
	}
	m.Subject(message.Subject)
	m.SetDate()
	m.SetMessageID()
	if message.BodyType == EmailBodyTypeText {
		m.SetBodyString(gomail.TypeTextPlain, message.Body)
	} else {
		m.SetBodyString(gomail.TypeTextHTML, message.Body)
	}
	return m, nil
}

func validateAddressList(field string, values []string) error {
	for _, value := range values {
		if err := validateSingleAddress(field, value); err != nil {
			return err
		}
	}
	return nil
}

func validateSingleAddress(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if hasCRLF(value) {
		return fmt.Errorf("%s contains invalid line breaks", field)
	}
	if _, err := netmail.ParseAddress(value); err != nil {
		return fmt.Errorf("%s must be a valid email address: %w", field, err)
	}
	return nil
}

func normalizeRecipientSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func optionalInt(values map[string]interface{}, key string) (int, error) {
	if values == nil {
		return 0, nil
	}
	value, ok := values[key]
	if !ok || value == nil {
		return 0, nil
	}
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case float32:
		return int(typed), nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, nil
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", key)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be a number", key)
	}
}

func stringValue(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func hasCRLF(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

var _ SMTPSender = (*GoMailSMTPSender)(nil)
