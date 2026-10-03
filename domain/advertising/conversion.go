package advertising

import (
	"strings"
	"time"
)

type Pixel struct {
	MetaID        string     `json:"metaId"`
	Name          string     `json:"name"`
	LastFiredTime *time.Time `json:"lastFiredTime,omitempty"`
	CreationTime  *time.Time `json:"creationTime,omitempty"`
	Unavailable   bool       `json:"unavailable"`
}

type ConversionSettings struct {
	WorkspaceID   string    `json:"-"`
	AdAccountID   string    `json:"adAccountId"`
	DatasetID     string    `json:"datasetId,omitempty"`
	PixelID       string    `json:"pixelId,omitempty"`
	SendLeads     bool      `json:"sendLeads"`
	SendPurchases bool      `json:"sendPurchases"`
	Enabled       bool      `json:"enabled"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func (s ConversionSettings) Validate() error {
	v := newIssues()
	if strings.TrimSpace(s.AdAccountID) == "" {
		v.add("adAccountId", "required")
	}
	if s.Enabled && strings.TrimSpace(s.DatasetID) == "" && strings.TrimSpace(s.PixelID) == "" {
		v.add("datasetId", "required")
	}
	if s.Enabled && !s.SendLeads && !s.SendPurchases {
		v.add("sendLeads", "nothing_to_send")
	}
	return v.err()
}

type MessagingChannel string

const (
	ChannelWhatsApp  MessagingChannel = "whatsapp"
	ChannelMessenger MessagingChannel = "messenger"
	ChannelInstagram MessagingChannel = "instagram"
)

type MessagingIdentity struct {
	Channel          MessagingChannel
	ClickID          string
	WABAID           string
	PageID           string
	PageScopedUserID string
	InstagramUserID  string
	InstagramScoped  string
}

func (m MessagingIdentity) Complete() bool {
	switch m.Channel {
	case ChannelWhatsApp:
		return m.ClickID != "" && m.WABAID != ""
	case ChannelMessenger:
		return m.PageID != "" && m.PageScopedUserID != ""
	case ChannelInstagram:
		return m.InstagramUserID != "" && m.InstagramScoped != ""
	}
	return false
}

type DealEvent string

const (
	DealCreated DealEvent = "created"
	DealWon     DealEvent = "won"
)

type DealSignal struct {
	OpportunityID string
	Event         DealEvent
	At            time.Time
	ValueCents    int64
	Currency      string
	Identity      MessagingIdentity
	Phone         string
	Email         string
}

const (
	EventNameLead     = "LeadSubmitted"
	EventNamePurchase = "Purchase"
	conversionMaxAge  = 7 * 24 * time.Hour
)

type ConversionTarget string

const (
	TargetDataset ConversionTarget = "dataset"
	TargetPixel   ConversionTarget = "pixel"
)

type ConversionEvent struct {
	OpportunityID string
	EventID       string
	Name          string
	Time          time.Time
	Target        ConversionTarget
	TargetID      string
	ActionSource  string
	Identity      MessagingIdentity
	PhoneHash     string
	EmailHash     string
	ValueMicros   int64
	Currency      string
}

type SkipReason string

const (
	SkipNotEnabled   SkipReason = "not_enabled"
	SkipEventOff     SkipReason = "event_off"
	SkipNoAdIdentity SkipReason = "no_ad_identity"
	SkipTooOld       SkipReason = "too_old"
	SkipNoDataset    SkipReason = "no_dataset"
	SkipValueMissing SkipReason = "value_missing"
)

func ConversionFor(settings ConversionSettings, signal DealSignal, now time.Time) (*ConversionEvent, SkipReason) {
	if !settings.Enabled {
		return nil, SkipNotEnabled
	}
	name := ""
	switch {
	case signal.Event == DealCreated && settings.SendLeads:
		name = EventNameLead
	case signal.Event == DealWon && settings.SendPurchases:
		name = EventNamePurchase
	default:
		return nil, SkipEventOff
	}
	if now.Sub(signal.At) > conversionMaxAge {
		return nil, SkipTooOld
	}
	event := &ConversionEvent{OpportunityID: signal.OpportunityID, EventID: signal.OpportunityID + ":" + name, Name: name, Time: signal.At}
	switch {
	case signal.Identity.Complete() && strings.TrimSpace(settings.DatasetID) != "":
		event.Target, event.TargetID, event.ActionSource, event.Identity = TargetDataset, settings.DatasetID, "business_messaging", signal.Identity
	case strings.TrimSpace(settings.PixelID) != "" && (signal.Phone != "" || signal.Email != ""):
		event.Target, event.TargetID, event.ActionSource = TargetPixel, settings.PixelID, "system_generated"
		event.PhoneHash = HashMatch(MatchPhone, signal.Phone, "")
		event.EmailHash = HashMatch(MatchEmail, signal.Email, "")
		if event.PhoneHash == "" && event.EmailHash == "" {
			return nil, SkipNoAdIdentity
		}
	case strings.TrimSpace(settings.DatasetID) == "" && strings.TrimSpace(settings.PixelID) == "":
		return nil, SkipNoDataset
	default:
		return nil, SkipNoAdIdentity
	}
	if name == EventNamePurchase {
		if signal.ValueCents <= 0 || signal.Currency == "" {
			return nil, SkipValueMissing
		}
		event.ValueMicros = signal.ValueCents * centsToMicros
		event.Currency = strings.ToUpper(signal.Currency)
	}
	return event, ""
}

type ConversionStatus string

const (
	ConversionSending ConversionStatus = "sending"
	ConversionSent    ConversionStatus = "sent"
	ConversionSkipped ConversionStatus = "skipped"
	ConversionFailed  ConversionStatus = "failed"
)

type ConversionRecord struct {
	OpportunityID string           `json:"opportunityId"`
	EventName     string           `json:"eventName"`
	WorkspaceID   string           `json:"-"`
	Status        ConversionStatus `json:"status"`
	Reason        string           `json:"reason,omitempty"`
	Attempts      int              `json:"attempts"`
	SentAt        *time.Time       `json:"sentAt,omitempty"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

const maxConversionAttempts = 5

func (r ConversionRecord) Retryable() bool {
	return r.Status == ConversionFailed && r.Attempts < maxConversionAttempts
}
