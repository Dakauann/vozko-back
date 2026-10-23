package emailapihttp

import (
	"encoding/json"
	"time"

	"vozko/domain/emailapi"
	"vozko/domain/emailwebhook"
	emailapi_usecase "vozko/usecases/emailapi"
	emailwebhook_usecase "vozko/usecases/emailwebhook"
)

type TagRequest struct {
	Name  string `json:"name" example:"kind"`
	Value string `json:"value" example:"password_reset"`
}

type AttachmentRequest struct {
	Filename    string `json:"filename" example:"nota-fiscal.pdf"`
	Content     string `json:"content,omitempty" example:"JVBERi0xLjQK..."`
	ContentType string `json:"content_type,omitempty" example:"application/pdf"`
	Path        string `json:"path,omitempty" example:"https://arquivos.exemplo.com.br/nota.pdf"`
}

type SendRequest struct {
	From           string              `json:"from" example:"no-reply@acme.com.br"`
	To             []string            `json:"to" example:"ana@cliente.com.br"`
	Cc             []string            `json:"cc,omitempty"`
	Bcc            []string            `json:"bcc,omitempty"`
	ReplyTo        []string            `json:"reply_to,omitempty" example:"suporte@acme.com.br"`
	Subject        string              `json:"subject" example:"Seu código de acesso"`
	HTML           string              `json:"html,omitempty" example:"<p>Seu código é <b>482913</b></p>"`
	Text           string              `json:"text,omitempty" example:"Seu código é 482913"`
	DesignID       string              `json:"design_id,omitempty"`
	Variables      map[string]string   `json:"variables,omitempty"`
	Headers        map[string]string   `json:"headers,omitempty"`
	Tags           []TagRequest        `json:"tags,omitempty"`
	Attachments    []AttachmentRequest `json:"attachments,omitempty"`
	IdempotencyKey string              `json:"idempotency_key" example:"reset-7f3c2a"`
}

type MessageOutcome struct {
	ID        string `json:"id"`
	To        string `json:"to" example:"ana@cliente.com.br"`
	Role      string `json:"role" example:"to"`
	Status    string `json:"status" example:"sent"`
	ErrorCode string `json:"error_code,omitempty" example:"email_recipient_suppressed"`
}

type SendResponse struct {
	ID                string           `json:"id"`
	IdempotencyKey    string           `json:"idempotency_key"`
	Repeated          bool             `json:"repeated"`
	Messages          []MessageOutcome `json:"messages"`
	RetryAfterSeconds int64            `json:"retry_after_seconds,omitempty"`
}

func sendResponse(r emailapi_usecase.Result) SendResponse {
	out := SendResponse{ID: r.Send.ID, IdempotencyKey: r.Send.IdempotencyKey, Repeated: r.Repeated, RetryAfterSeconds: r.RetryAfterSeconds, Messages: make([]MessageOutcome, len(r.Messages))}
	for i, m := range r.Messages {
		out.Messages[i] = MessageOutcome{ID: m.ID, To: string(m.Address), Role: string(m.Role), Status: string(m.Status), ErrorCode: m.ErrorCode}
	}
	return out
}

type LogEntryResponse struct {
	ID             string            `json:"id"`
	SendID         string            `json:"send_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	Origin         string            `json:"origin" example:"api"`
	From           string            `json:"from"`
	To             string            `json:"to"`
	Role           string            `json:"role" example:"to"`
	Subject        string            `json:"subject"`
	Tags           map[string]string `json:"tags"`
	Status         string            `json:"status" example:"delivered"`
	ErrorCode      string            `json:"error_code,omitempty"`
	LastEvent      string            `json:"last_event,omitempty" example:"delivery"`
	LastEventAt    *time.Time        `json:"last_event_at"`
	CreatedAt      time.Time         `json:"created_at"`
}

type LogPageResponse struct {
	Data       []LogEntryResponse `json:"data"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

type EventResponse struct {
	Kind       string          `json:"kind" example:"delivery"`
	OccurredAt time.Time       `json:"occurred_at"`
	Link       string          `json:"link,omitempty"`
	IsBot      bool            `json:"is_bot"`
	Detail     json.RawMessage `json:"detail,omitempty" swaggertype:"object"`
}

type LogDetailResponse struct {
	LogEntryResponse
	Events []EventResponse `json:"events"`
}

func logEntry(e emailapi.LogEntry) LogEntryResponse {
	tags := e.Send.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	return LogEntryResponse{
		ID: e.ID, SendID: e.SendID, IdempotencyKey: e.Send.IdempotencyKey, Origin: string(e.Send.Origin), From: string(e.Send.From),
		To: string(e.Address), Role: string(e.Role), Subject: e.Send.Subject, Tags: tags, Status: string(e.Status), ErrorCode: e.ErrorCode,
		LastEvent: e.LastEvent, LastEventAt: e.LastEventAt, CreatedAt: e.CreatedAt,
	}
}

func logPage(p emailapi.Page[emailapi.LogEntry]) LogPageResponse {
	out := LogPageResponse{NextCursor: p.Next, Data: make([]LogEntryResponse, len(p.Items))}
	for i, e := range p.Items {
		out.Data[i] = logEntry(e)
	}
	return out
}

func logDetail(d emailapi.Detail) LogDetailResponse {
	out := LogDetailResponse{LogEntryResponse: logEntry(d.Entry), Events: make([]EventResponse, len(d.Events))}
	for i, ev := range d.Events {
		out.Events[i] = EventResponse{Kind: ev.Kind, OccurredAt: ev.OccurredAt, Link: ev.Link, IsBot: ev.IsBot}
		if len(ev.Detail) > 0 && json.Valid(ev.Detail) {
			out.Events[i].Detail = json.RawMessage(ev.Detail)
		}
	}
	return out
}

type EndpointRequest struct {
	URL     string   `json:"url" example:"https://api.acme.com.br/webhooks/email"`
	Events  []string `json:"events" example:"email.delivered,email.bounced"`
	Enabled *bool    `json:"enabled,omitempty"`
}

type EndpointUpdateRequest struct {
	URL     *string  `json:"url,omitempty"`
	Events  []string `json:"events,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
}

type EndpointResponse struct {
	ID            string     `json:"id"`
	URL           string     `json:"url"`
	Events        []string   `json:"events"`
	Enabled       bool       `json:"enabled"`
	SecretHint    string     `json:"secret_hint" example:"x9Qa"`
	FailingSince  *time.Time `json:"failing_since"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastFailureAt *time.Time `json:"last_failure_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type EndpointWithSecretResponse struct {
	EndpointResponse
	Secret string `json:"secret" example:"whsec_3qT0..."`
}

type EndpointListResponse struct {
	Endpoints []EndpointResponse `json:"endpoints"`
	Events    []string           `json:"events"`
	Limit     int                `json:"limit" example:"10"`
}

type AttemptResponse struct {
	Attempt       int        `json:"attempt"`
	StatusCode    int        `json:"status_code"`
	DurationMS    int64      `json:"duration_ms"`
	Error         string     `json:"error,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at"`
	AttemptedAt   time.Time  `json:"attempted_at"`
}

type DeliveryResponse struct {
	ID             string            `json:"id"`
	EventID        string            `json:"event_id"`
	EventType      string            `json:"event_type" example:"email.delivered"`
	MessageID      string            `json:"message_id"`
	Status         string            `json:"status" example:"retrying"`
	Attempts       int               `json:"attempts"`
	NextAttemptAt  *time.Time        `json:"next_attempt_at"`
	LastStatusCode int               `json:"last_status_code"`
	LastError      string            `json:"last_error,omitempty"`
	Retryable      bool              `json:"retryable"`
	CreatedAt      time.Time         `json:"created_at"`
	DeliveredAt    *time.Time        `json:"delivered_at"`
	AttemptLog     []AttemptResponse `json:"attempt_log"`
}

type DeliveryPageResponse struct {
	Data       []DeliveryResponse `json:"data"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

func endpointResponse(e emailwebhook.Endpoint) EndpointResponse {
	events := make([]string, len(e.Events))
	for i, ev := range e.Events {
		events[i] = string(ev)
	}
	return EndpointResponse{ID: e.ID, URL: e.URL, Events: events, Enabled: e.Enabled, SecretHint: e.SecretHint, FailingSince: e.FailingSince,
		LastSuccessAt: e.LastSuccessAt, LastFailureAt: e.LastFailureAt, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}

func deliveryResponse(v emailwebhook_usecase.DeliveryView) DeliveryResponse {
	d := v.Delivery
	out := DeliveryResponse{ID: d.ID, EventID: d.EventID, EventType: string(d.EventType), MessageID: d.MessageID, Status: string(d.Status), Attempts: d.Attempts,
		NextAttemptAt: d.NextAttemptAt, LastStatusCode: d.LastStatusCode, LastError: d.LastError, Retryable: d.Retryable(), CreatedAt: d.CreatedAt,
		DeliveredAt: d.DeliveredAt, AttemptLog: make([]AttemptResponse, len(v.Attempts))}
	for i, a := range v.Attempts {
		out.AttemptLog[i] = AttemptResponse{Attempt: a.Attempt, StatusCode: a.StatusCode, DurationMS: a.DurationMS, Error: a.Error, NextAttemptAt: a.NextAttemptAt, AttemptedAt: a.AttemptedAt}
	}
	return out
}
