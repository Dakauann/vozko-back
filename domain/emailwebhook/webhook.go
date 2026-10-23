package emailwebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
)

const (
	MaxEndpointsPerWorkspace = 10
	MaxURLLength             = 2048
	MaxAttempts              = 8
	SecretPrefix             = "whsec_"
	secretBytes              = 32
	SignatureTolerance       = 5 * time.Minute
	FailingAlertAfter        = time.Hour
	AlertEvery               = time.Hour
	MaxInFlightPerEndpoint   = 4
	SlotTTL                  = 30 * time.Second
	SlotWait                 = 5 * time.Second
	RequestTimeout           = 10 * time.Second
	SendingStaleAfter        = 2 * time.Minute
	QueuedStaleAfter         = 5 * time.Minute
	SweepBatch               = 200
	Retention                = 30 * 24 * time.Hour
	RetentionChunk           = 5000
	RetentionMaxChunks       = 40
	MaxErrorLength           = 500
)

var (
	ErrURLInvalid           = errors.New("emailwebhook: the url must be https, with a host, without credentials or fragment")
	ErrURLBlocked           = errors.New("emailwebhook: the url resolves to a private, local or reserved address")
	ErrEventsInvalid        = errors.New("emailwebhook: choose at least one known event")
	ErrEndpointNotFound     = errors.New("emailwebhook: endpoint not found")
	ErrLimitReached         = errors.New("emailwebhook: the workspace reached the number of endpoints")
	ErrDeliveryNotFound     = errors.New("emailwebhook: delivery not found")
	ErrDeliveryNotRetryable = errors.New("emailwebhook: only failed or skipped deliveries can be sent again")
	ErrSignatureInvalid     = errors.New("emailwebhook: signature does not verify")
	ErrRandom               = errors.New("emailwebhook: the random source failed")
)

type EventType string

const (
	EventSent            EventType = "email.sent"
	EventDelivered       EventType = "email.delivered"
	EventDeliveryDelayed EventType = "email.delivery_delayed"
	EventBounced         EventType = "email.bounced"
	EventComplained      EventType = "email.complained"
	EventOpened          EventType = "email.opened"
	EventClicked         EventType = "email.clicked"
	EventFailed          EventType = "email.failed"
)

var EventTypes = []EventType{EventSent, EventDelivered, EventDeliveryDelayed, EventBounced, EventComplained, EventOpened, EventClicked, EventFailed}

var eventByKind = map[es.EventKind]EventType{
	es.EventSend:             EventSent,
	es.EventDelivery:         EventDelivered,
	es.EventDeliveryDelay:    EventDeliveryDelayed,
	es.EventBounce:           EventBounced,
	es.EventComplaint:        EventComplained,
	es.EventOpen:             EventOpened,
	es.EventClick:            EventClicked,
	es.EventReject:           EventFailed,
	es.EventRenderingFailure: EventFailed,
}

func EventTypeOf(ev es.DeliveryEvent) (EventType, bool) {
	if ev.IsBot {
		return "", false
	}
	t, ok := eventByKind[ev.Kind]
	return t, ok
}

func (t EventType) Valid() bool {
	for _, known := range EventTypes {
		if t == known {
			return true
		}
	}
	return false
}

func EventID(messageID string, kind es.EventKind, occurredAt time.Time, link string) string {
	sum := sha256.Sum256([]byte(messageID + "\x00" + string(kind) + "\x00" + occurredAt.UTC().Format(time.RFC3339Nano) + "\x00" + link))
	return "evt_" + hex.EncodeToString(sum[:16])
}

func ValidateURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > MaxURLLength || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", ErrURLInvalid
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || strings.Contains(trimmed, "#") {
		return "", ErrURLInvalid
	}
	return trimmed, nil
}

func HostOf(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func ValidateEvents(raw []string) ([]EventType, error) {
	seen := map[EventType]bool{}
	var out []EventType
	for _, r := range raw {
		t := EventType(r)
		if !t.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrEventsInvalid, r)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, ErrEventsInvalid
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

type Endpoint struct {
	ID            string
	WorkspaceID   string
	URL           string
	Events        []EventType
	Enabled       bool
	SecretCipher  []byte
	SecretHint    string
	FailingSince  *time.Time
	AlertedAt     *time.Time
	LastSuccessAt *time.Time
	LastFailureAt *time.Time
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (e Endpoint) Wants(t EventType) bool {
	if !e.Enabled {
		return false
	}
	for _, have := range e.Events {
		if have == t {
			return true
		}
	}
	return false
}

func (e Endpoint) AlertDue(now time.Time) bool {
	if e.FailingSince == nil || now.Sub(*e.FailingSince) < FailingAlertAfter {
		return false
	}
	return e.AlertedAt == nil || now.Sub(*e.AlertedAt) >= AlertEvery
}

func AlertKey(endpointID string) string { return "email_webhook_failing:" + endpointID }

type DeliveryStatus string

const (
	StatusQueued    DeliveryStatus = "queued"
	StatusSending   DeliveryStatus = "sending"
	StatusRetrying  DeliveryStatus = "retrying"
	StatusDelivered DeliveryStatus = "delivered"
	StatusFailed    DeliveryStatus = "failed"
	StatusSkipped   DeliveryStatus = "skipped"
)

type Delivery struct {
	ID             string
	EndpointID     string
	WorkspaceID    string
	EventID        string
	EventType      EventType
	MessageID      string
	Payload        []byte
	Status         DeliveryStatus
	Attempts       int
	NextAttemptAt  *time.Time
	LastStatusCode int
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeliveredAt    *time.Time
}

func (d Delivery) Keyset() emailapi.Cursor { return emailapi.Cursor{CreatedAt: d.CreatedAt, ID: d.ID} }

func (d Delivery) Retryable() bool { return d.Status == StatusFailed || d.Status == StatusSkipped }

type Outcome struct {
	Status        DeliveryStatus
	NextAttemptAt *time.Time
}

func (d Delivery) AfterAttempt(success bool, now time.Time) Outcome {
	if success {
		return Outcome{Status: StatusDelivered}
	}
	wait, ok := Backoff(d.Attempts)
	if !ok {
		return Outcome{Status: StatusFailed}
	}
	next := now.Add(wait)
	return Outcome{Status: StatusRetrying, NextAttemptAt: &next}
}

var backoffs = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour}

func Backoff(attempt int) (time.Duration, bool) {
	if attempt < 1 || attempt >= MaxAttempts || attempt > len(backoffs) {
		return 0, false
	}
	return backoffs[attempt-1], true
}

type Attempt struct {
	DeliveryID    string
	Attempt       int
	StatusCode    int
	DurationMS    int64
	Error         string
	NextAttemptAt *time.Time
	AttemptedAt   time.Time
}

func ClipError(s string) string {
	if len(s) <= MaxErrorLength {
		return s
	}
	return s[:MaxErrorLength]
}

func GenerateSecret(random io.Reader) (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := io.ReadFull(random, buf); err != nil {
		return "", fmt.Errorf("%w: %v", ErrRandom, err)
	}
	return SecretPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func Hint(secret string) string {
	if len(secret) < 4 {
		return ""
	}
	return secret[len(secret)-4:]
}

func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func SignatureHeader(secret string, timestamp int64, body []byte) string {
	return "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + Sign(secret, timestamp, body)
}

func Verify(secret, header string, body []byte, now time.Time) error {
	var timestamp int64
	var signature string
	for _, part := range strings.Split(header, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return ErrSignatureInvalid
			}
			timestamp = parsed
		case "v1":
			signature = value
		}
	}
	if timestamp == 0 || signature == "" {
		return ErrSignatureInvalid
	}
	age := now.Sub(time.Unix(timestamp, 0))
	if age > SignatureTolerance || age < -SignatureTolerance {
		return ErrSignatureInvalid
	}
	if !hmac.Equal([]byte(signature), []byte(Sign(secret, timestamp, body))) {
		return ErrSignatureInvalid
	}
	return nil
}

type bouncePayload struct {
	Type       string `json:"type"`
	SubType    string `json:"sub_type,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type complaintPayload struct {
	FeedbackType string `json:"feedback_type,omitempty"`
}

type clickPayload struct {
	Link string `json:"link"`
}

type dataPayload struct {
	MessageID      string            `json:"message_id"`
	SendID         string            `json:"send_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	From           string            `json:"from"`
	To             string            `json:"to"`
	Role           string            `json:"role"`
	Subject        string            `json:"subject"`
	Tags           map[string]string `json:"tags"`
	OccurredAt     time.Time         `json:"occurred_at"`
	Bounce         *bouncePayload    `json:"bounce,omitempty"`
	Complaint      *complaintPayload `json:"complaint,omitempty"`
	Click          *clickPayload     `json:"click,omitempty"`
}

type eventPayload struct {
	ID        string      `json:"id"`
	Type      EventType   `json:"type"`
	CreatedAt time.Time   `json:"created_at"`
	Data      dataPayload `json:"data"`
}

func BuildPayload(eventID string, t EventType, entry emailapi.LogEntry, ev es.DeliveryEvent, createdAt time.Time) ([]byte, error) {
	tags := entry.Send.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	data := dataPayload{
		MessageID:      entry.ID,
		SendID:         entry.SendID,
		IdempotencyKey: entry.Send.IdempotencyKey,
		From:           string(entry.Send.From),
		To:             string(entry.Address),
		Role:           string(entry.Role),
		Subject:        entry.Send.Subject,
		Tags:           tags,
		OccurredAt:     ev.OccurredAt.UTC(),
	}
	if ev.Bounce != nil {
		data.Bounce = &bouncePayload{Type: string(ev.Bounce.Kind), SubType: ev.Bounce.SubType, Diagnostic: ev.Bounce.Diagnostic}
	}
	if ev.Complaint != nil {
		data.Complaint = &complaintPayload{FeedbackType: ev.Complaint.FeedbackType}
	}
	if t == EventClicked {
		data.Click = &clickPayload{Link: ev.Link}
	}
	return json.Marshal(eventPayload{ID: eventID, Type: t, CreatedAt: createdAt.UTC(), Data: data})
}
