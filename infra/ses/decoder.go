package ses

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

var ErrUnknownNotification = errors.New("ses: unknown notification shape")

const (
	tenantNameTag          = "ses:tenant-name"
	topicValidationMessage = "Successfully validated SNS topic for Amazon SES event publishing."
	receiptSetupMessageID  = "AMAZON_SES_SETUP_NOTIFICATION"
	receiptSetupRecipient  = "recipient@example.com"
)

type Decoder struct{}

var _ emailsending.NotificationDecoder = Decoder{}

type envelopeProbe struct {
	EventType        string `json:"eventType"`
	NotificationType string `json:"notificationType"`
	Source           string `json:"source"`
	DetailType       string `json:"detail-type"`
}

func (Decoder) Decode(payload []byte) (emailsending.Notification, error) {
	if string(payload) == topicValidationMessage {
		return emailsending.Notification{Notice: &emailsending.ProviderNotice{Kind: emailsending.NoticeTopicValidated}}, nil
	}
	var probe envelopeProbe
	if err := json.Unmarshal(payload, &probe); err != nil {
		return emailsending.Notification{}, fmt.Errorf("%w: %v", ErrUnknownNotification, err)
	}
	switch {
	case probe.EventType != "":
		ev, err := decodeEvent(payload, probe.EventType)
		if err != nil {
			return emailsending.Notification{}, err
		}
		return emailsending.Notification{Delivery: ev}, nil
	case probe.NotificationType == "Received":
		return decodeReceived(payload)
	case probe.Source == "aws.ses" && probe.DetailType != "":
		return decodeEventBridge(payload, probe.DetailType)
	}
	return emailsending.Notification{}, fmt.Errorf("%w: no event, receipt or eventbridge marker", ErrUnknownNotification)
}

type sesRecipient struct {
	EmailAddress   string `json:"emailAddress"`
	DiagnosticCode string `json:"diagnosticCode"`
}

type sesEvent struct {
	Mail struct {
		Timestamp   string              `json:"timestamp"`
		MessageID   string              `json:"messageId"`
		Destination []string            `json:"destination"`
		Tags        map[string][]string `json:"tags"`
	} `json:"mail"`
	Bounce *struct {
		BounceType        string         `json:"bounceType"`
		BounceSubType     string         `json:"bounceSubType"`
		BouncedRecipients []sesRecipient `json:"bouncedRecipients"`
		Timestamp         string         `json:"timestamp"`
	} `json:"bounce"`
	Complaint *struct {
		ComplainedRecipients  []sesRecipient `json:"complainedRecipients"`
		Timestamp             string         `json:"timestamp"`
		UserAgent             string         `json:"userAgent"`
		ComplaintFeedbackType string         `json:"complaintFeedbackType"`
	} `json:"complaint"`
	Delivery *struct {
		Timestamp  string   `json:"timestamp"`
		Recipients []string `json:"recipients"`
	} `json:"delivery"`
	DeliveryDelay *struct {
		Timestamp         string         `json:"timestamp"`
		DelayedRecipients []sesRecipient `json:"delayedRecipients"`
	} `json:"deliveryDelay"`
	Open *struct {
		Timestamp  string `json:"timestamp"`
		UserAgent  string `json:"userAgent"`
		IsBotEvent string `json:"isBotEvent"`
	} `json:"open"`
	Click *struct {
		Timestamp  string `json:"timestamp"`
		UserAgent  string `json:"userAgent"`
		Link       string `json:"link"`
		IsBotEvent string `json:"isBotEvent"`
	} `json:"click"`
}

var eventKinds = map[string]emailsending.EventKind{
	"Send":              emailsending.EventSend,
	"Delivery":          emailsending.EventDelivery,
	"Bounce":            emailsending.EventBounce,
	"Complaint":         emailsending.EventComplaint,
	"Open":              emailsending.EventOpen,
	"Click":             emailsending.EventClick,
	"Reject":            emailsending.EventReject,
	"DeliveryDelay":     emailsending.EventDeliveryDelay,
	"Rendering Failure": emailsending.EventRenderingFailure,
	"RenderingFailure":  emailsending.EventRenderingFailure,
}

func decodeEvent(payload []byte, eventType string) (*emailsending.DeliveryEvent, error) {
	kind, ok := eventKinds[eventType]
	if !ok {
		return nil, fmt.Errorf("%w: event type %q", ErrUnknownNotification, eventType)
	}
	var raw sesEvent
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnknownNotification, err)
	}
	if strings.TrimSpace(raw.Mail.MessageID) == "" {
		return nil, fmt.Errorf("%w: event without message id", ErrUnknownNotification)
	}
	ev := &emailsending.DeliveryEvent{
		Kind:              kind,
		ProviderMessageID: raw.Mail.MessageID,
		Tags:              firstTagValues(raw.Mail.Tags),
	}
	ev.TenantName = ev.Tags[tenantNameTag]
	recipients := raw.Mail.Destination
	stamp := raw.Mail.Timestamp
	switch kind {
	case emailsending.EventBounce:
		if raw.Bounce == nil {
			return nil, fmt.Errorf("%w: bounce without details", ErrUnknownNotification)
		}
		stamp = firstNonEmpty(raw.Bounce.Timestamp, stamp)
		recipients = recipientAddresses(raw.Bounce.BouncedRecipients, recipients)
		ev.Bounce = &emailsending.Bounce{
			Kind:       emailsending.BounceKindOf(raw.Bounce.BounceType, raw.Bounce.BounceSubType),
			SubType:    raw.Bounce.BounceSubType,
			Diagnostic: firstDiagnostic(raw.Bounce.BouncedRecipients),
		}
	case emailsending.EventComplaint:
		if raw.Complaint == nil {
			return nil, fmt.Errorf("%w: complaint without details", ErrUnknownNotification)
		}
		stamp = firstNonEmpty(raw.Complaint.Timestamp, stamp)
		recipients = recipientAddresses(raw.Complaint.ComplainedRecipients, recipients)
		ev.UserAgent = raw.Complaint.UserAgent
		ev.Complaint = &emailsending.Complaint{FeedbackType: raw.Complaint.ComplaintFeedbackType}
	case emailsending.EventDelivery:
		if raw.Delivery != nil {
			stamp = firstNonEmpty(raw.Delivery.Timestamp, stamp)
			if len(raw.Delivery.Recipients) > 0 {
				recipients = raw.Delivery.Recipients
			}
		}
	case emailsending.EventDeliveryDelay:
		if raw.DeliveryDelay != nil {
			stamp = firstNonEmpty(raw.DeliveryDelay.Timestamp, stamp)
			recipients = recipientAddresses(raw.DeliveryDelay.DelayedRecipients, recipients)
		}
	case emailsending.EventOpen:
		if raw.Open == nil {
			return nil, fmt.Errorf("%w: open without details", ErrUnknownNotification)
		}
		stamp = firstNonEmpty(raw.Open.Timestamp, stamp)
		ev.UserAgent = raw.Open.UserAgent
		ev.IsBot = likelyBot(raw.Open.IsBotEvent)
	case emailsending.EventClick:
		if raw.Click == nil {
			return nil, fmt.Errorf("%w: click without details", ErrUnknownNotification)
		}
		stamp = firstNonEmpty(raw.Click.Timestamp, stamp)
		ev.UserAgent = raw.Click.UserAgent
		ev.Link = raw.Click.Link
		ev.IsBot = likelyBot(raw.Click.IsBotEvent)
	}
	at, err := parseStamp(stamp)
	if err != nil {
		return nil, err
	}
	ev.OccurredAt = at
	ev.Recipients = normalizeAll(recipients)
	return ev, nil
}

type sesReceipt struct {
	Mail struct {
		Timestamp   string   `json:"timestamp"`
		MessageID   string   `json:"messageId"`
		Destination []string `json:"destination"`
	} `json:"mail"`
	Receipt struct {
		Timestamp    string     `json:"timestamp"`
		Recipients   []string   `json:"recipients"`
		SpamVerdict  sesVerdict `json:"spamVerdict"`
		VirusVerdict sesVerdict `json:"virusVerdict"`
		Action       struct {
			Type      string `json:"type"`
			ObjectKey string `json:"objectKey"`
		} `json:"action"`
	} `json:"receipt"`
}

type sesVerdict struct {
	Status string `json:"status"`
}

func (v sesVerdict) verdict() emailsending.Verdict {
	return emailsending.Verdict(strings.ToUpper(strings.TrimSpace(v.Status)))
}

func decodeReceived(payload []byte) (emailsending.Notification, error) {
	var raw sesReceipt
	if err := json.Unmarshal(payload, &raw); err != nil {
		return emailsending.Notification{}, fmt.Errorf("%w: %v", ErrUnknownNotification, err)
	}
	if raw.receiptSetup() {
		return emailsending.Notification{Notice: &emailsending.ProviderNotice{Kind: emailsending.NoticeReceiptSetup}}, nil
	}
	in, err := decodeReceipt(raw)
	if err != nil {
		return emailsending.Notification{}, err
	}
	return emailsending.Notification{Inbound: in}, nil
}

func (r sesReceipt) receiptSetup() bool {
	only := func(addresses []string) bool {
		return len(addresses) == 1 && addresses[0] == receiptSetupRecipient
	}
	return r.Mail.MessageID == receiptSetupMessageID && only(r.Mail.Destination) && only(r.Receipt.Recipients)
}

func decodeReceipt(raw sesReceipt) (*emailsending.InboundNotice, error) {
	if raw.Receipt.Action.Type != "S3" {
		return nil, fmt.Errorf("%w: receipt action %q", ErrUnknownNotification, raw.Receipt.Action.Type)
	}
	if !emailsending.ValidRawMessageKey(raw.Receipt.Action.ObjectKey) {
		return nil, fmt.Errorf("%w: object key %q", ErrUnknownNotification, raw.Receipt.Action.ObjectKey)
	}
	if strings.TrimSpace(raw.Mail.MessageID) == "" {
		return nil, fmt.Errorf("%w: receipt without message id", ErrUnknownNotification)
	}
	at, err := parseStamp(firstNonEmpty(raw.Receipt.Timestamp, raw.Mail.Timestamp))
	if err != nil {
		return nil, err
	}
	recipients := raw.Receipt.Recipients
	if len(recipients) == 0 {
		recipients = raw.Mail.Destination
	}
	normalized := normalizeAll(recipients)
	if len(normalized) == 0 {
		return nil, fmt.Errorf("%w: receipt without recipients", ErrUnknownNotification)
	}
	return &emailsending.InboundNotice{
		Recipients: normalized,
		ObjectKey:  raw.Receipt.Action.ObjectKey,
		MessageID:  raw.Mail.MessageID,
		ReceivedAt: at,
		Verdict:    emailsending.InboundVerdict{Spam: raw.Receipt.SpamVerdict.verdict(), Virus: raw.Receipt.VirusVerdict.verdict()},
	}, nil
}

type eventBridgeEvent struct {
	Time      string   `json:"time"`
	Resources []string `json:"resources"`
	Detail    struct {
		Data     json.RawMessage `json:"data"`
		Metadata struct {
			Impact string `json:"impact"`
			Type   string `json:"type"`
		} `json:"metadata"`
	} `json:"detail"`
}

type statusData struct {
	Origin string `json:"origin"`
	Record struct {
		Status string `json:"status"`
		Cause  string `json:"cause"`
	} `json:"record"`
}

var statusOrigins = map[string]emailsending.TenantStatusOrigin{
	"CUSTOMER_MANAGED": emailsending.OriginPlatform,
	"AWS_SES":          emailsending.OriginProvider,
}

var sendingStatuses = map[string]emailsending.TenantSendingStatus{
	"ENABLED":    emailsending.TenantEnabled,
	"DISABLED":   emailsending.TenantDisabled,
	"REINSTATED": emailsending.TenantReinstated,
}

var findingImpacts = map[string]emailsending.FindingImpact{
	"HIGH": emailsending.FindingHigh,
	"LOW":  emailsending.FindingLow,
}

func decodeEventBridge(payload []byte, detailType string) (emailsending.Notification, error) {
	var raw eventBridgeEvent
	if err := json.Unmarshal(payload, &raw); err != nil {
		return emailsending.Notification{}, fmt.Errorf("%w: %v", ErrUnknownNotification, err)
	}
	tenant, err := tenantFromResources(raw.Resources)
	if err != nil {
		return emailsending.Notification{}, err
	}
	at, err := parseStamp(raw.Time)
	if err != nil {
		return emailsending.Notification{}, err
	}
	switch detailType {
	case "Sending Status Enabled", "Sending Status Disabled":
		var data statusData
		if err := json.Unmarshal(raw.Detail.Data, &data); err != nil {
			return emailsending.Notification{}, fmt.Errorf("%w: status data: %v", ErrUnknownNotification, err)
		}
		origin, ok := statusOrigins[data.Origin]
		if !ok {
			return emailsending.Notification{}, fmt.Errorf("%w: status origin %q", ErrUnknownNotification, data.Origin)
		}
		status, ok := sendingStatuses[data.Record.Status]
		if !ok {
			return emailsending.Notification{}, fmt.Errorf("%w: sending status %q", ErrUnknownNotification, data.Record.Status)
		}
		return emailsending.Notification{TenantStatus: &emailsending.TenantStatusEvent{
			TenantName: tenant,
			Origin:     origin,
			Status:     status,
			Cause:      data.Record.Cause,
			OccurredAt: at,
		}}, nil
	case "Advisor Recommendation Status Open", "Advisor Recommendation Status Resolved":
		impact, ok := findingImpacts[strings.ToUpper(raw.Detail.Metadata.Impact)]
		if !ok {
			return emailsending.Notification{}, fmt.Errorf("%w: finding impact %q", ErrUnknownNotification, raw.Detail.Metadata.Impact)
		}
		var description string
		if len(raw.Detail.Data) > 0 {
			if err := json.Unmarshal(raw.Detail.Data, &description); err != nil {
				return emailsending.Notification{}, fmt.Errorf("%w: finding data: %v", ErrUnknownNotification, err)
			}
		}
		return emailsending.Notification{Finding: &emailsending.FindingEvent{
			TenantName:  tenant,
			Impact:      impact,
			Type:        raw.Detail.Metadata.Type,
			Description: description,
			Open:        strings.HasSuffix(detailType, "Open"),
			OccurredAt:  at,
		}}, nil
	}
	return emailsending.Notification{}, fmt.Errorf("%w: detail type %q", ErrUnknownNotification, detailType)
}

func tenantFromResources(resources []string) (string, error) {
	for _, arn := range resources {
		parts := strings.SplitN(arn, ":", 6)
		if len(parts) != 6 || parts[2] != "ses" {
			continue
		}
		segments := strings.Split(parts[5], "/")
		if len(segments) >= 2 && segments[0] == "tenant" && segments[1] != "" {
			return segments[1], nil
		}
	}
	return "", fmt.Errorf("%w: no tenant resource", ErrUnknownNotification)
}

func parseStamp(raw string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil || at.IsZero() {
		return time.Time{}, fmt.Errorf("%w: timestamp %q", ErrUnknownNotification, raw)
	}
	return at.UTC(), nil
}

func firstTagValues(tags map[string][]string) map[string]string {
	out := make(map[string]string, len(tags))
	for k, values := range tags {
		if len(values) > 0 {
			out[k] = values[0]
		}
	}
	return out
}

func recipientAddresses(recipients []sesRecipient, fallback []string) []string {
	if len(recipients) == 0 {
		return fallback
	}
	out := make([]string, 0, len(recipients))
	for _, r := range recipients {
		out = append(out, r.EmailAddress)
	}
	return out
}

func firstDiagnostic(recipients []sesRecipient) string {
	for _, r := range recipients {
		if r.DiagnosticCode != "" {
			return r.DiagnosticCode
		}
	}
	return ""
}

func normalizeAll(raw []string) []emailaddress.Address {
	out := make([]emailaddress.Address, 0, len(raw))
	for _, r := range raw {
		if a, err := emailaddress.Normalize(r); err == nil {
			out = append(out, a)
		}
	}
	return out
}

func likelyBot(flag string) bool {
	return strings.EqualFold(strings.TrimSpace(flag), "Likely")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
