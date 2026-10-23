package emailsending

import (
	"strings"
	"time"

	"vozko/domain/emailaddress"
)

type EventKind string

const (
	EventSend             EventKind = "send"
	EventDelivery         EventKind = "delivery"
	EventBounce           EventKind = "bounce"
	EventComplaint        EventKind = "complaint"
	EventOpen             EventKind = "open"
	EventClick            EventKind = "click"
	EventReject           EventKind = "reject"
	EventDeliveryDelay    EventKind = "delivery_delay"
	EventRenderingFailure EventKind = "rendering_failure"
)

type BounceKind string

const (
	BounceHard       BounceKind = "hard"
	BounceSoft       BounceKind = "soft"
	BounceSuppressed BounceKind = "suppressed"
)

type Bounce struct {
	Kind       BounceKind
	SubType    string
	Diagnostic string
}

type Complaint struct {
	FeedbackType string
}

type DeliveryEvent struct {
	Kind              EventKind
	ProviderMessageID string
	Recipients        []emailaddress.Address
	OccurredAt        time.Time
	TenantName        string
	Bounce            *Bounce
	Complaint         *Complaint
	Link              string
	UserAgent         string
	IsBot             bool
	Tags              map[string]string
}

func BounceKindOf(bounceType, subType string) BounceKind {
	if !strings.EqualFold(bounceType, "Permanent") {
		return BounceSoft
	}
	switch subType {
	case "Suppressed", "OnAccountSuppressionList", "OnTenantSuppressionList", "EmailValidationSuppressed", "UnsubscribedRecipient":
		return BounceSuppressed
	}
	return BounceHard
}

type TenantSendingStatus string

const (
	TenantEnabled    TenantSendingStatus = "enabled"
	TenantDisabled   TenantSendingStatus = "disabled"
	TenantReinstated TenantSendingStatus = "reinstated"
)

type TenantStatusOrigin string

const (
	OriginPlatform TenantStatusOrigin = "platform"
	OriginProvider TenantStatusOrigin = "provider"
)

type TenantStatusEvent struct {
	TenantName string
	Origin     TenantStatusOrigin
	Status     TenantSendingStatus
	Cause      string
	OccurredAt time.Time
}

type FindingImpact string

const (
	FindingHigh FindingImpact = "high"
	FindingLow  FindingImpact = "low"
)

type FindingEvent struct {
	TenantName  string
	Impact      FindingImpact
	Type        string
	Description string
	Open        bool
	OccurredAt  time.Time
}

type InboundNotice struct {
	Recipients []emailaddress.Address
	ObjectKey  string
	MessageID  string
	ReceivedAt time.Time
	Verdict    InboundVerdict
}

type NoticeKind string

const (
	NoticeTopicValidated NoticeKind = "topic_validated"
	NoticeReceiptSetup   NoticeKind = "receipt_setup"
)

type ProviderNotice struct {
	Kind NoticeKind
}

type Notification struct {
	Delivery     *DeliveryEvent
	TenantStatus *TenantStatusEvent
	Finding      *FindingEvent
	Inbound      *InboundNotice
	Notice       *ProviderNotice
}
