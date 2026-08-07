package unofficial_whatsapp

import (
	"context"
	"errors"
	"strings"
	"time"
)

// The outbound provider surface.
//
// Every call is addressed by an explicit ref rather than by client state,
// because a workspace can connect several numbers across several hosts and
// binding the credential to the call is what guarantees a reply leaves from the
// same number the message arrived on.
//
// The surface is split by responsibility rather than being one god-interface:
// provisioning needs a host-wide admin token, everything else needs an instance
// token, and a test double for the health cron should not have to implement
// sending.

// ServerRef addresses one provider host with its admin credential.
type ServerRef struct {
	BaseURL string
	// AdminToken is host-wide: it can create and delete every instance on the
	// host. Only the provisioning path ever holds one.
	AdminToken string
}

// InstanceRef addresses one instance with its own credential.
type InstanceRef struct {
	BaseURL string
	Token   string
}

// RefFor builds an InstanceRef from an instance and its host.
func RefFor(server *Server, instance *Instance) InstanceRef {
	if server == nil || instance == nil {
		return InstanceRef{}
	}
	return InstanceRef{BaseURL: server.BaseURL, Token: instance.InstanceToken}
}

// ---------------------------------------------------------------- lifecycle

// CreatedInstance is the host's answer to a provisioning request.
type CreatedInstance struct {
	ProviderInstanceID string
	Token              string
	Name               string
}

// CreateInstanceInput provisions one instance on a host.
type CreateInstanceInput struct {
	Name string
	// WorkspaceID and OurInstanceID are stored in the host's own admin-only
	// metadata slots. They are the trace that lets an orphaned instance on a
	// host be matched back to a tenant, which is the only way a capacity
	// reconciliation can tell a leak from a legitimate row.
	WorkspaceID   string
	OurInstanceID string
}

// ConnectMode is how a number is linked.
type ConnectMode string

const (
	// ConnectModeQR renders a QR code the customer scans from their phone.
	ConnectModeQR ConnectMode = "qr"
	// ConnectModePairing sends a code the customer types into their phone.
	// It needs the number up front, and its deadline is longer than the QR's.
	ConnectModePairing ConnectMode = "pairing"
)

func (m ConnectMode) Valid() bool { return m == ConnectModeQR || m == ConnectModePairing }

// Connect deadlines, from the provider's documentation. They are surfaced to
// the operator rather than guessed, because a screen that stalls silently past
// an expiry is indistinguishable from one that is broken.
const (
	QRCodeTTL      = 2 * time.Minute
	PairingCodeTTL = 5 * time.Minute
)

// ConnectInput starts a linking attempt.
type ConnectInput struct {
	Mode ConnectMode
	// Phone is required for ConnectModePairing and ignored otherwise.
	Phone string
	// SystemName appears in the phone's "linked devices" list. Left empty by
	// default: the provider documents that its own default is more stable.
	SystemName string
}

// Session is the host's view of an instance at one moment: the state machine
// plus whatever linking material is currently live.
type Session struct {
	// State is the provider's raw state string, mapped by the caller. Kept raw
	// here so an unrecognised value can be logged rather than silently coerced
	// into one we do know.
	State     string
	Connected bool
	LoggedIn  bool

	// QRCode is a data URI, valid only until it rotates.
	QRCode   string
	PairCode string

	JID           string
	LID           string
	ProfileName   string
	ProfilePicURL string
	IsBusiness    bool
	Platform      string

	LastDisconnectAt     *time.Time
	LastDisconnectReason string
}

// MapState translates the provider's state string onto our lifecycle.
//
// It never invents a status for an unknown value: an unrecognised state returns
// ok=false so the caller can log it and leave the row alone, rather than
// reporting a live session as disconnected because the vendor added a state.
func MapState(state string, connected bool) (Status, bool) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "connected":
		return StatusConnected, true
	case "connecting":
		return StatusAwaitingScan, true
	case "hibernated":
		return StatusHibernated, true
	case "disconnected":
		return StatusDisconnected, true
	}
	// Some hosts answer with an empty state and only the booleans.
	if connected {
		return StatusConnected, true
	}
	return "", false
}

// InstanceAPI is the instance lifecycle.
type InstanceAPI interface {
	// CreateInstance provisions on a host. Admin-credentialed.
	CreateInstance(ctx context.Context, server ServerRef, in CreateInstanceInput) (*CreatedInstance, error)
	// ListInstances enumerates a host's instances, for capacity reconciliation
	// and orphan detection. Admin-credentialed.
	ListInstances(ctx context.Context, server ServerRef) ([]RemoteInstance, error)

	Connect(ctx context.Context, ref InstanceRef, in ConnectInput) (*Session, error)
	Status(ctx context.Context, ref InstanceRef) (*Session, error)
	Disconnect(ctx context.Context, ref InstanceRef) error
	// Reset restarts a wedged runtime without deleting the session. The host
	// enforces a cooldown between resets, so a caller must treat a refusal as
	// normal rather than as a failure.
	Reset(ctx context.Context, ref InstanceRef) error
	DeleteInstance(ctx context.Context, ref InstanceRef) error
}

// RemoteInstance is one row of a host's instance list.
type RemoteInstance struct {
	ProviderInstanceID string
	Name               string
	State              string
	// The admin metadata slots we wrote at provisioning time.
	WorkspaceID   string
	OurInstanceID string
}

// ---------------------------------------------------------------- webhooks

// WebhookSubscription is our webhook registration on one instance.
type WebhookSubscription struct {
	URL     string
	Enabled bool
	Events  []string
	// ExcludeMessages is deliberately empty in our registration; see
	// SubscribedEvents for why.
	ExcludeMessages []string
}

// SubscribedEvents is the event set we register.
//
// Two omissions and one inclusion are decisions rather than defaults:
//
//   - `presence` is NOT subscribed. It is the highest-volume event the provider
//     emits and it has no CRM meaning.
//   - `groups` and `newsletter_messages` are NOT subscribed. Groups are stored
//     inert (see Conversation.RunsAutomation) and newsletters are a publishing
//     surface we do not attend.
//   - `messages_update` IS subscribed, and no exclusion filter is set. The
//     provider's docs recommend excluding messages the API itself sent, to
//     break automation loops. Doing that would cost the delivery-status track
//     AND every message an operator types on their own phone, both of which are
//     the point of this channel. The loop is closed structurally instead: an
//     echo is recognised by its correlation id, rejected by the unique index on
//     (entry_type, external_message_id), and never enters the AI or workflow
//     paths because those refuse outbound messages.
func SubscribedEvents() []string {
	return []string{
		"messages",
		"messages_update",
		"connection",
		"chats",
		"contacts",
		"blocks",
		"call",
		"history",
		"labels",
		"chat_labels",
	}
}

// WebhookDeliveryError is one failed delivery the host recorded.
//
// The host keeps only the last handful, in memory, and loses them on restart,
// so reading these is the only forensic window this channel offers. There is no
// replay endpoint: a delivery that failed past its retries is gone.
type WebhookDeliveryError struct {
	At         time.Time
	URL        string
	Event      string
	StatusCode int
	Attempts   int
	Error      string
}

// WebhookAPI configures and inspects webhook delivery.
type WebhookAPI interface {
	SetWebhook(ctx context.Context, ref InstanceRef, sub WebhookSubscription) error
	GetWebhooks(ctx context.Context, ref InstanceRef) ([]WebhookSubscription, error)
	WebhookErrors(ctx context.Context, ref InstanceRef) ([]WebhookDeliveryError, error)
}

// ---------------------------------------------------------------- diagnostics

// DiagnosticsAPI reads the provider's view of WhatsApp's own limits.
type DiagnosticsAPI interface {
	// MessagingLimits reports whether WhatsApp is currently restricting new
	// conversations from this number, and it is the earliest warning available
	// before a ban.
	MessagingLimits(ctx context.Context, ref InstanceRef) (*Restriction, error)
	// DisableBuiltInChatbot switches off the provider's own AI answering.
	//
	// Not hygiene: the host ships a chatbot with its own model key, and if it is
	// on, two AI brains answer the same customer and neither knows about the
	// other. Asserted at provisioning and re-asserted by the health cron,
	// because a tenant with console access to the host can turn it back on.
	DisableBuiltInChatbot(ctx context.Context, ref InstanceRef) error
}

// ProviderAPI is the whole provider surface, for wiring convenience.
type ProviderAPI interface {
	InstanceAPI
	WebhookAPI
	DiagnosticsAPI
}

// ---------------------------------------------------------------- errors

// ProviderError is a structured provider failure.
//
// The fields beyond the HTTP status exist because this provider forwards
// WhatsApp's own refusals, and those are the ones that matter: a 463 is not a
// transport error to retry, it is WhatsApp saying the number is being limited,
// and treating it as retryable is how a warning becomes a ban.
type ProviderError struct {
	HTTPStatus int
	Message    string
	// ErrorSource distinguishes the host's own failures from WhatsApp's.
	ErrorSource string
	// ProviderCode is WhatsApp's code, forwarded verbatim.
	ProviderCode int
	ErrorKey     string
	// LocalizedMessage is the provider's own pt-BR text. Surfaced to operators
	// rather than re-translated: it describes WhatsApp's state, and inventing
	// our own words for it would drift from what the customer can verify.
	LocalizedMessage string
	// Restriction carries the parsed limit detail when WhatsApp sent one.
	Restriction *Restriction
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return "unofficial whatsapp provider error: " + e.Message
	}
	return "unofficial whatsapp provider error"
}

// whatsAppRestrictionCode is WhatsApp's "cannot start new conversations" code,
// forwarded by the provider as provider_code.
const whatsAppRestrictionCode = 463

// IsRestriction reports whether WhatsApp itself refused, as opposed to the host
// failing. It is the signal that must pause a broadcast rather than retry it.
func (e *ProviderError) IsRestriction() bool {
	if e == nil {
		return false
	}
	return e.ProviderCode == whatsAppRestrictionCode || e.Restriction != nil
}

// Retryable reports whether repeating the call could succeed.
//
// A restriction is explicitly NOT retryable even though it arrives with a 4xx
// that might otherwise look transient: retrying into a WhatsApp limit is the
// behaviour that escalates a temporary block into a permanent one.
func (e *ProviderError) Retryable() bool {
	if e == nil || e.IsRestriction() {
		return false
	}
	// 429 from the host means its instance ceiling, not WhatsApp's; 503 is its
	// documented transient capacity refusal.
	return e.HTTPStatus == 429 || e.HTTPStatus == 503 || e.HTTPStatus >= 500
}

// NeedsReconnect reports whether the credential or the session is gone. 401 is
// the only way an instance token dies: it does not expire, the instance is
// deleted or reset on the host.
func (e *ProviderError) NeedsReconnect() bool {
	return e != nil && e.HTTPStatus == 401
}

// AtCapacity reports whether the HOST refused for lack of room, which is a
// placement problem and not the tenant's fault.
func (e *ProviderError) AtCapacity() bool {
	return e != nil && (e.HTTPStatus == 429 || e.HTTPStatus == 503)
}

// AsProviderError extracts a structured provider failure, if this is one.
func AsProviderError(err error) (*ProviderError, bool) {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return provErr, true
	}
	return nil, false
}
