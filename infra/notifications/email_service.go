package notification_service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"vozko/domain/notification"

	"github.com/resend/resend-go/v3"
	"golang.org/x/time/rate"
)

const (
	emailSendPerAttemptTimeout = 12 * time.Second
	emailSendOverallTimeout    = 30 * time.Second
	emailSendMaxAttempts       = 4
	emailSendBaseBackoff       = 500 * time.Millisecond
	emailSendMaxBackoff        = 8 * time.Second

	defaultResendMaxRPS = 4
)

type EmailService struct {
	client          *resend.Client
	fromEmail       string
	fromName        string
	templatesLoader notification.TemplateLoader
	limiter         *rate.Limiter
	maxAttempts     int
}

func NewEmailService(templatesLoader notification.TemplateLoader, apiKey, fromEmail, fromName string, maxRPS int) notification.EmailService {
	apiKey = strings.TrimSpace(apiKey)

	var client *resend.Client
	if apiKey != "" {
		client = resend.NewClient(apiKey)
	}

	if maxRPS <= 0 {
		maxRPS = defaultResendMaxRPS
	}

	return &EmailService{
		client:          client,
		fromEmail:       strings.TrimSpace(fromEmail),
		fromName:        strings.TrimSpace(fromName),
		templatesLoader: templatesLoader,
		limiter:         rate.NewLimiter(rate.Limit(maxRPS), maxRPS),
		maxAttempts:     emailSendMaxAttempts,
	}
}

func (e *EmailService) SendEmail(to, subject, body string) error {
	if e.client == nil {
		return fmt.Errorf("Resend not configured - email sending disabled (set RESEND_API_KEY)")
	}

	recipients := parseRecipients(to)
	if len(recipients) == 0 {
		return fmt.Errorf("email recipient is required")
	}

	req := &resend.SendEmailRequest{
		From:    e.from(),
		To:      recipients,
		Subject: subject,
		Html:    body,
	}

	ctx, cancel := context.WithTimeout(context.Background(), emailSendOverallTimeout)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= e.maxAttempts; attempt++ {
		if err := e.limiter.Wait(ctx); err != nil {
			if lastErr != nil {
				return fmt.Errorf("failed to send email via Resend after %d attempts: %w", attempt-1, lastErr)
			}
			return fmt.Errorf("failed to send email via Resend: %w", err)
		}

		attemptCtx, attemptCancel := context.WithTimeout(ctx, emailSendPerAttemptTimeout)
		_, err := e.client.Emails.SendWithContext(attemptCtx, req)
		attemptCancel()
		if err == nil {
			return nil
		}
		lastErr = err

		if !isRetryableSendError(err) {
			return fmt.Errorf("failed to send email via Resend: %w", err)
		}
		if attempt == e.maxAttempts {
			break
		}

		timer := time.NewTimer(sendBackoff(attempt, err))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("failed to send email via Resend after %d attempts: %w", attempt, lastErr)
		case <-timer.C:
		}
	}

	return fmt.Errorf("failed to send email via Resend after %d attempts: %w", e.maxAttempts, lastErr)
}

func (e *EmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	if e.templatesLoader == nil {
		return fmt.Errorf("template loader not configured")
	}

	body, err := e.templatesLoader.LoadTemplate(templateName, data)
	if err != nil {
		return fmt.Errorf("failed to load template: %w", err)
	}

	return e.SendEmail(to, subject, body)
}

func isRetryableSendError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, resend.ErrRateLimit) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func sendBackoff(attempt int, err error) time.Duration {
	var rle *resend.RateLimitError
	if errors.As(err, &rle) {
		if secs, perr := strconv.Atoi(strings.TrimSpace(rle.RetryAfter)); perr == nil && secs > 0 {
			if d := time.Duration(secs) * time.Second; d < emailSendMaxBackoff {
				return d
			}
			return emailSendMaxBackoff
		}
	}
	if attempt < 1 {
		attempt = 1
	}
	backoff := emailSendBaseBackoff << (attempt - 1)
	if backoff > emailSendMaxBackoff {
		return emailSendMaxBackoff
	}
	return backoff
}

func (e *EmailService) from() string {
	if e.fromName != "" {
		return fmt.Sprintf("%s <%s>", e.fromName, e.fromEmail)
	}
	return e.fromEmail
}

func parseRecipients(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
