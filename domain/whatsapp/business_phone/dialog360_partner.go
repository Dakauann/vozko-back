package businessphone

// This file defines the domain port for the 360dialog Partner API
// (hub.360dialog.io/api/v2). It is the provisioning and management plane used to
// onboard new client channels under the partner credit line. The runtime
// messaging plane stays on the existing WhatsAppClient port (360dialog wraps the
// Meta Cloud API, so messaging differs only by base URL and credential header).

// Dialog360Channel is a channel as returned by the Partner API channel listing.
// Field mapping to Meta concepts: WABAExternalID is the Meta WABA id,
// PhoneNumber is the WhatsApp display number, and ID is the 360dialog channel id.
type Dialog360Channel struct {
	ID             string
	WABAExternalID string
	PhoneNumber    string
	// Metadata carried from the partner channel listing so a dialog360-hosted number
	// can be populated at finalize time. These numbers have no Meta access token, so
	// the Meta-Graph sync (SyncPhoneNumberUseCase) can never fetch them, the partner
	// channel is the only source. Fields map to the 360dialog channel response:
	// PhoneName=setup_info.phone_name, QualityRating=current_quality_rating,
	// MessagingTier=current_limit, ReviewStatus=waba_account.status.
	PhoneName     string
	QualityRating string
	MessagingTier string
	ReviewStatus  string
	// WABAName is the human account name (waba_account.on_behalf_of_business_info.name),
	// used to backfill the local WABA record so the number stops rendering as
	// "Conta não identificada". Empty until Meta/360dialog provide it.
	WABAName string
	Status   string
	// Deactivation signals used by the channel-status reconcile: HubStatus is the
	// channel's hub_status ("live", "pending_deletion", ...); Cancelled is true when
	// cancelled_at is set; AvailabilityStatus is "ready"/"error". A number whose
	// channel is deactivated must be flipped to SUSPENDED locally so it stops being
	// treated as live (and its now-invalid API key is never used).
	HubStatus          string
	Cancelled          bool
	AvailabilityStatus string
	// IsOnBizApp is true when the number is also on the WhatsApp Business App
	// (coexistence). It mirrors the channel's is_on_biz_app field.
	IsOnBizApp bool
}

// IsDeactivated reports whether 360dialog no longer treats this channel as a live,
// usable channel, the local number should then be SUSPENDED. Conservative: only
// terminal signals count (a submitted cancellation, a pending-deletion/terminated
// hub state, or the BSP removed from the WABA), not a transient availability blip.
func (c Dialog360Channel) IsDeactivated() bool {
	if c.Cancelled {
		return true
	}
	switch c.HubStatus {
	case "pending_deletion", "deleted", "terminated", "cancelled":
		return true
	}
	switch c.ReviewStatus {
	case "bsp_removed", "disabled", "banned":
		return true
	}
	return false
}

// Dialog360Balance is the partner shared prepaid balance (the credit line sits
// on top of this). Read only: 360dialog exposes no funding endpoint.
type Dialog360Balance struct {
	Amount   float64
	Currency string
}

// RegisterNumberInput shares a number onboarded via Embedded Signup with the
// 360dialog partner so 360dialog hosts and bills it.
type RegisterNumberInput struct {
	ClientID string
	// WABAExternalID is the Meta WhatsApp Business Account id from Embedded Signup.
	WABAExternalID string
	// ChannelExternalID is the Meta phone_number_id. Empty connects every number
	// in the WABA (per the Partner Hosted Embedded Signup docs).
	ChannelExternalID string
}

// APIKeyResult is returned when a channel key is generated. The key is shown
// once and must be stored immediately; Address is the messaging base URL.
type APIKeyResult struct {
	APIKey  string
	Address string
}

// Dialog360PartnerService is the Partner API plane. Implementations target
// hub.360dialog.io/api/v2 with the partner x-api-key header.
type Dialog360PartnerService interface {
	// CreateClient creates (or returns an existing) client and yields its id.
	CreateClient(name, email string) (clientID string, err error)
	// FindClientByEmail returns the id of an existing partner client whose contact
	// email matches, or "" if none exists. This is the idempotency key for client
	// creation: 360dialog can create the client yet still return an error to the
	// caller (observed 429/500 acks), so onboarding looks up the deterministic
	// per-workspace email before creating, otherwise every retry leaks a duplicate.
	FindClientByEmail(email string) (clientID string, err error)
	// RegisterNumber shares a Meta WABA number with the partner (account_sharing).
	RegisterNumber(input RegisterNumberInput) error
	// ListChannels lists every channel on the partner account (used by the
	// reconciliation job and to resolve the Meta field mapping after onboarding).
	ListChannels() ([]Dialog360Channel, error)
	// GetChannel fetches a single channel by id in one call (no full-fleet paging),
	// or nil if it is not present. Used by finalize so onboarding stays O(1).
	GetChannel(channelID string) (*Dialog360Channel, error)
	// GenerateAPIKey mints the per channel messaging key. One shot: the newest
	// key invalidates any previous one, and it is rate limited per channel.
	GenerateAPIKey(channelID string) (*APIKeyResult, error)
	// GetPartnerBalance reads the shared prepaid balance for monitoring.
	GetPartnerBalance() (*Dialog360Balance, error)
	// CancelChannel submits a graceful cancellation request so the partner stops
	// billing the channel at the end of the current calendar month (no proration,
	// reversible via ReactivateChannel until the channel is fully deactivated).
	// Used for addon-lapse suspension and orphaned-channel rollback. The endpoint
	// is client scoped, so the channel's owning clientID is required.
	CancelChannel(clientID, channelID string) error
	// ReactivateChannel revokes a pending cancellation, keeping the channel active.
	// Used when a suspended number's addon is re-purchased before month-end. The
	// endpoint is client scoped, so the channel's owning clientID is required.
	ReactivateChannel(clientID, channelID string) error
	// SetWebhookURL sets the partner lifecycle webhook URL.
	SetWebhookURL(webhookURL string) error
}
