package notification_service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"vozko/domain/business_metrics"
	"vozko/domain/notification"

	"github.com/google/uuid"
	"github.com/resend/resend-go/v3"
	"golang.org/x/time/rate"
)

const (
	// Per-attempt and overall bounds on a single SendEmail call. The overall
	// budget is kept modest on purpose: the notification queue uses prefetch=1,
	// so a long in-call retry would stall the whole queue. Transient failures
	// that outlive this budget are handed back to the consumer, which schedules
	// a delayed redelivery instead of blocking.
	emailSendPerAttemptTimeout = 12 * time.Second
	emailSendOverallTimeout    = 30 * time.Second
	emailSendMaxAttempts       = 4
	emailSendBaseBackoff       = 500 * time.Millisecond
	emailSendMaxBackoff        = 8 * time.Second

	// defaultResendMaxRPS stays under Resend's documented 5 req/s per-team limit.
	defaultResendMaxRPS = 4
)

// EmailService is the system transactional email sender, backed by Resend.
// It powers auth verification, password resets, order/ticket/workspace/payment
// notifications and the async email-notification consumer. The workflow
// "Send Email" node uses a separate per-call SMTP sender and is unaffected.
//
// Sends are rate limited client-side (token bucket) to avoid Resend 429s, and
// each send retries transient failures (rate limit, request timeout, network
// errors) with backoff that honours the API's Retry-After hint.
type EmailService struct {
	client          *resend.Client
	fromEmail       string
	fromName        string
	templatesLoader notification.TemplateLoader
	recordMetric    business_metrics.RecordMetricUseCase
	limiter         *rate.Limiter
	maxAttempts     int
}

// NewEmailService builds the Resend-backed email service. apiKey is the
// RESEND_API_KEY; fromEmail/fromName form the From header (the domain must be
// verified in Resend for production sends). maxRPS caps client-side send rate;
// pass <= 0 for the safe default.
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

func (e *EmailService) SetRecordMetric(recordMetric business_metrics.RecordMetricUseCase) {
	e.recordMetric = recordMetric
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
		// Stay under Resend's per-team rate limit; blocks until a token frees
		// up or the overall budget expires.
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
			e.recordEmailSent(to, subject)
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

// isRetryableSendError reports whether a failed Resend send is worth retrying.
// A 429 is surfaced by the SDK as a typed *resend.RateLimitError (matching
// resend.ErrRateLimit); request timeouts and transport errors are transient.
// Other API errors (4xx validation, unknown) are treated as permanent here;
// sustained 5xx outages are caught instead by the consumer's delayed requeue.
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

// sendBackoff returns how long to wait before the next attempt. On a rate limit
// it honours the API's Retry-After (capped); otherwise exponential backoff.
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

// from renders the From header, e.g. "Brand <no-reply@example.com>".
func (e *EmailService) from() string {
	if e.fromName != "" {
		return fmt.Sprintf("%s <%s>", e.fromName, e.fromEmail)
	}
	return e.fromEmail
}

// parseRecipients accepts a single address or a comma-separated list, mirroring
// the previous SMTP "To" header behaviour, and returns a clean slice.
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

func (e *EmailService) recordEmailSent(to, subject string) {
	if e.recordMetric == nil {
		return
	}

	err := e.recordMetric.Execute(business_metrics.RecordMetricInput{
		EventType:  business_metrics.EventEmailSent,
		EntityID:   uuid.New().String(),
		EntityType: business_metrics.EntityTypeEmail,
		Metadata: map[string]string{
			"to":      to,
			"subject": subject,
		},
	})

	if err != nil {
		log.Printf("failed to record email sent metric: %v", err)
	}
}
