// Package unofficial_whatsapp holds the contracts and rules for WhatsApp
// reached through a linked-device session rather than through Meta's Cloud API.
//
// It deliberately contains no HTTP: the vendor's wire format lives in
// infra/uazapi, and uazapi is one PROVIDER of this channel rather than the
// channel itself. Nothing in this package may name it.
//
// Three rules shape everything downstream, and none of them is inferable from a
// generic messaging model:
//
//  1. There is no messaging window and no template. Cold outbound works. The
//     composer closes only when the SESSION dies, when WhatsApp restricts the
//     number, or when the contact blocks it — never on a clock.
//  2. The contact is an E.164 phone number, so it maps onto a CRM lead. That is
//     the decisive difference from Instagram and Telegram, whose contacts are
//     opaque provider ids that no dialer, boleto or export can address.
//  3. The account can be BANNED. This is an unofficial transport, and abuse is
//     not punished with a rejected API call but with the customer losing their
//     number. Every rule below that looks conservative is that risk showing
//     through.
package unofficial_whatsapp

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrServerNotFound        = errors.New("unofficial whatsapp server not found")
	ErrNoServerCapacity      = errors.New("no unofficial whatsapp server has capacity for a new instance")
	ErrServerBaseURLRequired = errors.New("unofficial whatsapp server base url is required")
	ErrAdminTokenRequired    = errors.New("unofficial whatsapp server admin token is required")

	ErrInstanceNotFound     = errors.New("unofficial whatsapp instance not found")
	ErrInstanceNameRequired = errors.New("unofficial whatsapp instance name is required")
	ErrWorkspaceIDRequired  = errors.New("workspace id is required")
	ErrInvalidStatus        = errors.New("invalid unofficial whatsapp instance status")
	ErrStatusTransition     = errors.New("invalid unofficial whatsapp instance status transition")
	ErrInstanceNotConnected = errors.New("unofficial whatsapp instance is not connected")
	ErrNumberAlreadyLinked  = errors.New("this whatsapp number is already connected to a workspace")

	ErrContactNotFound      = errors.New("unofficial whatsapp contact not found")
	ErrContactBlocked       = errors.New("the contact has blocked this number")
	ErrConversationNotFound = errors.New("unofficial whatsapp conversation not found")

	ErrTextTooLong    = errors.New("unofficial whatsapp message text is too long")
	ErrPhoneRequired  = errors.New("a phone number is required")
	ErrPhoneInvalid   = errors.New("phone number must be in international format")
	ErrRestrictedByWA = errors.New("whatsapp is currently restricting new conversations from this number")
)

// MaxTextRunes bounds an outbound body.
//
// This is OUR cap, not the provider's: the vendor documents no text limit at
// all. WhatsApp's own client ceiling is 65536 characters, and a limit we chose
// is labelled as such rather than presented as a contract we can rely on.
const MaxTextRunes = 65536

// Media ceilings. Also OURS, and also conservative, for the same reason: the
// vendor documents no size limits, and discovering one by having a 90 MB upload
// rejected mid-send is a bad way to find out. Seeded from WhatsApp's published
// client limits.
const (
	MaxImageBytes    int64 = 16 << 20
	MaxVideoBytes    int64 = 16 << 20
	MaxAudioBytes    int64 = 16 << 20
	MaxDocumentBytes int64 = 100 << 20
)

// Interactive limits, from WhatsApp's own interactive message types. Unlike the
// sizes above these are the platform's, not ours: WhatsApp splits at three
// buttons into a different message type and caps a list at ten rows.
const (
	MaxButtonOptions      = 3
	MaxListOptions        = 10
	MaxOptionLabelRunes   = 24
	MaxOptionPayloadBytes = 256
)

// Default pacing between outbound sends on one instance.
//
// A constant cadence is the most legible bot signature there is, so sends are
// jittered inside a range rather than spaced by a fixed delay. These are the
// defaults an instance starts with; an operator can widen them and, deliberately,
// cannot narrow them below MinSendDelayMS.
const (
	DefaultSendDelayMinMS = 3000
	DefaultSendDelayMaxMS = 12000
	MinSendDelayMS        = 500
)

// ProviderUazapi is the first (and today only) provider of this channel.
//
// It is a value rather than an assumption so a second provider is another
// adapter behind the same ProviderAPI port, not a second entry type and a second
// pass over every registry in the CRM.
const ProviderUazapi = "uazapi"

// ---------------------------------------------------------------- server

// Server is one provider host we can place instances on.
//
// It is a row rather than a config string because a host has a hard ceiling on
// connected instances and answers 429 past it. Capacity that is not modelled is
// capacity that fails at the moment a customer clicks Connect, with an error
// nobody can explain.
type Server struct {
	ID string `json:"id"`
	// WorkspaceID is nil for a platform-owned host usable by any workspace, and
	// set for a bring-your-own host. Nil is the normal case: the admin token is
	// host-wide, so a tenant must never hold ours and we must never hold theirs.
	WorkspaceID *string `json:"workspaceId,omitempty"`

	Name     string `json:"name"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	// AdminToken is host-wide and never serialized. Encrypted at rest, and
	// rotatable, because it can create and delete every instance on the host.
	AdminToken string `json:"-"`

	Capacity int  `json:"capacity"`
	InUse    int  `json:"inUse"`
	Enabled  bool `json:"enabled"`
	// Draining stops new placements without evicting what is already running, so
	// a host can be retired without disconnecting live customers.
	Draining bool `json:"draining"`

	LastHealthyAt *time.Time `json:"lastHealthyAt,omitempty"`
	LastError     string     `json:"lastError,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Server) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.BaseURL = strings.TrimRight(strings.TrimSpace(s.BaseURL), "/")
	s.Provider = strings.TrimSpace(s.Provider)
	if s.Provider == "" {
		s.Provider = ProviderUazapi
	}
}

func (s *Server) Validate() error {
	if s.BaseURL == "" {
		return ErrServerBaseURLRequired
	}
	if strings.TrimSpace(s.AdminToken) == "" {
		return ErrAdminTokenRequired
	}
	return nil
}

// HasCapacity reports whether one more instance may be placed here.
//
// A zero Capacity means "unknown", not "unlimited", and is treated as full. The
// safe default matters: guessing unlimited means we keep placing instances onto
// a host that has already started refusing them.
func (s *Server) HasCapacity() bool {
	return s.Enabled && !s.Draining && s.Capacity > 0 && s.InUse < s.Capacity
}

// ---------------------------------------------------------------- instance

// Status is the instance lifecycle.
//
// The first four mirror the provider's own states; the last three are ours,
// because "the host says disconnected" and "Meta banned this number" need
// different copy, different alerting and different remedies, and the provider
// reports both the same way.
type Status string

const (
	// StatusProvisioning is between our row being created and the instance
	// existing on the host.
	StatusProvisioning Status = "PROVISIONING"
	// StatusAwaitingScan means a QR code or pairing code is live and unexpired.
	StatusAwaitingScan Status = "AWAITING_SCAN"
	StatusConnected    Status = "CONNECTED"
	// StatusHibernated is the provider's paused-but-credentialed state. It
	// reconnects without a new scan, which is why it is not DISCONNECTED.
	StatusHibernated Status = "HIBERNATED"
	// StatusDisconnected means the session is gone and a new scan is required.
	StatusDisconnected Status = "DISCONNECTED"
	// StatusBanned means WhatsApp has disabled the number. Terminal: no
	// reconnection recovers it, and the UI must say so rather than offering a
	// Reconnect button that can only fail.
	StatusBanned Status = "BANNED"
	// StatusProvisionFailed means the host never created the instance.
	StatusProvisionFailed Status = "PROVISION_FAILED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusProvisioning, StatusAwaitingScan, StatusConnected, StatusHibernated,
		StatusDisconnected, StatusBanned, StatusProvisionFailed:
		return true
	}
	return false
}

// CanTransitionTo guards the lifecycle, following the precedent set by the two
// existing channels of having a guard from day one rather than bare assignment.
func (s Status) CanTransitionTo(next Status) bool {
	if !next.Valid() {
		return false
	}
	if s == next {
		return true
	}
	switch s {
	case StatusProvisioning:
		return next == StatusAwaitingScan || next == StatusProvisionFailed
	case StatusAwaitingScan:
		// A scan can expire back to DISCONNECTED without ever connecting.
		return next == StatusConnected || next == StatusDisconnected ||
			next == StatusProvisionFailed || next == StatusBanned
	case StatusConnected:
		return next == StatusHibernated || next == StatusDisconnected || next == StatusBanned
	case StatusHibernated:
		return next == StatusConnected || next == StatusDisconnected || next == StatusBanned
	case StatusDisconnected:
		return next == StatusAwaitingScan || next == StatusConnected || next == StatusBanned
	case StatusProvisionFailed:
		// Retrying provisioning is the only way out.
		return next == StatusProvisioning
	case StatusBanned:
		// Terminal by design.
		return false
	}
	return false
}

// Terminal reports whether no automatic recovery is possible.
func (s Status) Terminal() bool { return s == StatusBanned }

// Restriction is WhatsApp's own limiting state for the connected number,
// cached from the provider's diagnostics endpoint.
//
// Load-bearing, not decoration: every send checks it, and WhatsApp signals a
// restriction before it bans. Ignoring the signal is the difference between a
// number that stops sending for a while and one that is gone.
type Restriction struct {
	// CanSendNewChats is nil when the diagnosis could not be completed. Nil is
	// not "yes": an unknown answer must not authorise a blast.
	CanSendNewChats *bool      `json:"canSendNewChats,omitempty"`
	Key             string     `json:"key,omitempty"`
	Message         string     `json:"message,omitempty"`
	Until           *time.Time `json:"until,omitempty"`
	// UsedQuota / TotalQuota mirror the new-conversation cap when WhatsApp
	// reports one, so the UI can show "8 de 10" instead of a bare refusal.
	UsedQuota  int        `json:"usedQuota,omitempty"`
	TotalQuota int        `json:"totalQuota,omitempty"`
	CheckedAt  *time.Time `json:"checkedAt,omitempty"`
}

// Active reports whether WhatsApp is currently restricting new conversations.
//
// It fails CLOSED on an unknown answer only when a restriction window is still
// running; a never-checked instance is not treated as restricted, or no instance
// could ever send its first message.
func (r Restriction) Active(now time.Time) bool {
	if r.Until != nil && now.Before(*r.Until) {
		return true
	}
	return r.CanSendNewChats != nil && !*r.CanSendNewChats
}

// Instance is one connected WhatsApp number.
//
// It doubles as the config carrier for its conversations, the role
// whatsapp_campaigns plays for the Cloud API and telegram_accounts plays for
// Telegram, which is why the automation fields live here.
type Instance struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`
	ServerID     string  `json:"serverId"`
	Provider     string  `json:"provider"`

	// ProviderInstanceID is the host's own id. It is what an inbound webhook
	// body carries, and therefore the second factor that makes forging one need
	// more than the delivery URL.
	ProviderInstanceID string `json:"providerInstanceId"`
	ProviderName       string `json:"providerName,omitempty"`
	// InstanceToken authenticates every call for this instance. It never
	// expires and grants full control of the customer's WhatsApp, so it is
	// encrypted at rest and never serialized.
	InstanceToken string `json:"-"`

	// DeliveryToken is the unguessable path segment of our webhook URL.
	//
	// It is the ONLY authenticity control this channel has: the provider does
	// not sign webhook bodies and offers no way to send a header of ours. A
	// bearer credential in a URL is a weak control, which is why it is
	// rotatable, stored as a digest for lookup, and never logged.
	DeliveryToken     string     `json:"-"`
	DeliveryTokenHash string     `json:"-"`
	WebhookSetAt      *time.Time `json:"webhookSetAt,omitempty"`

	// The connected WhatsApp account.
	JID            string `json:"jid,omitempty"`
	LID            string `json:"lid,omitempty"`
	PhoneNumber    string `json:"phoneNumber,omitempty"`
	ProfileName    string `json:"profileName,omitempty"`
	ProfilePicURL  string `json:"profilePicUrl,omitempty"`
	IsBusinessAcct bool   `json:"isBusinessAccount"`
	Platform       string `json:"platform,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`

	Status       Status `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	ConnectedAt       *time.Time `json:"connectedAt,omitempty"`
	LastDisconnectAt  *time.Time `json:"lastDisconnectAt,omitempty"`
	LastDisconnectWhy string     `json:"lastDisconnectReason,omitempty"`
	// LastPolledAt is when the health cron last reconciled with the host. A
	// stale value IS the alarm: this channel fails silently, and "quiet inbox"
	// and "session died an hour ago" look identical without it.
	LastPolledAt *time.Time `json:"lastPolledAt,omitempty"`

	Restriction Restriction `json:"restriction"`

	// Ban-risk controls. Nothing in the provider's API offers these; they are
	// ours, and they are the highest-value protection in the channel.
	DailySendCap    int        `json:"dailySendCap"`
	WarmupStartedAt *time.Time `json:"warmupStartedAt,omitempty"`
	SendDelayMinMS  int        `json:"sendDelayMinMs"`
	SendDelayMaxMS  int        `json:"sendDelayMaxMs"`
	AutoRejectCalls bool       `json:"autoRejectCalls"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	// HandleGroups is off by default and is a decision, not an oversight: a
	// group conversation is stored and visible but runs no automation, because
	// an agent answering a group thread is answering the wrong audience.
	HandleGroups bool `json:"handleGroups"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (i *Instance) Normalize() {
	i.ProviderName = strings.TrimSpace(i.ProviderName)
	i.ProfileName = strings.TrimSpace(i.ProfileName)
	i.DisplayName = strings.TrimSpace(i.DisplayName)
	i.JID = strings.TrimSpace(i.JID)
	i.LID = strings.TrimSpace(i.LID)
	i.PhoneNumber = NormalizePhone(i.PhoneNumber)

	if i.Provider == "" {
		i.Provider = ProviderUazapi
	}
	if i.Status == "" {
		i.Status = StatusProvisioning
	}
	// Each bound falls back independently. Defaulting only the minimum and then
	// clamping would collapse an unset range to min..min, i.e. a FIXED cadence —
	// the exact machine-regular rhythm the jitter exists to avoid, applied to
	// every instance that never touched the setting.
	if i.SendDelayMinMS <= 0 {
		i.SendDelayMinMS = DefaultSendDelayMinMS
	}
	if i.SendDelayMaxMS <= 0 {
		i.SendDelayMaxMS = DefaultSendDelayMaxMS
	}
	if i.SendDelayMaxMS < i.SendDelayMinMS {
		i.SendDelayMaxMS = i.SendDelayMinMS
	}
}

func (i *Instance) Validate() error {
	if strings.TrimSpace(i.WorkspaceID) == "" {
		return ErrWorkspaceIDRequired
	}
	if strings.TrimSpace(i.ServerID) == "" {
		return ErrServerNotFound
	}
	if !i.Status.Valid() {
		return ErrInvalidStatus
	}
	if i.SendDelayMinMS < MinSendDelayMS {
		return ErrInvalidStatus
	}
	return nil
}

// Label is what the CRM shows for this instance. It never falls back to a
// provider id: an operator choosing which number to reply from must see a
// number, not an opaque handle.
func (i *Instance) Label() string {
	if i.DisplayName != "" {
		return i.DisplayName
	}
	if i.ProfileName != "" {
		return i.ProfileName
	}
	if i.PhoneNumber != "" {
		return "+" + i.PhoneNumber
	}
	return i.ProviderName
}

// SessionLive reports whether the host currently holds a usable session.
func (i *Instance) SessionLive() bool {
	return i.Status == StatusConnected
}

// CanSend reports whether outbound is permitted right now, and why not.
//
// The three refusals are distinguished rather than collapsed into a bool
// because they need different words and different remedies in the UI:
// reconnect, wait, or nothing-you-can-do.
func (i *Instance) CanSend(now time.Time) (bool, error) {
	if !i.SessionLive() {
		return false, ErrInstanceNotConnected
	}
	if i.Restriction.Active(now) {
		return false, ErrRestrictedByWA
	}
	return true, nil
}

// SendDelayRange returns the jitter bounds for one outbound send, clamped so a
// misconfigured instance cannot send at machine speed.
func (i *Instance) SendDelayRange() (minMS, maxMS int) {
	minMS, maxMS = i.SendDelayMinMS, i.SendDelayMaxMS
	if minMS < MinSendDelayMS {
		minMS = MinSendDelayMS
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	return minMS, maxMS
}

// ---------------------------------------------------------------- contact

// Contact is a person on the other side of one of our numbers.
//
// WhatsApp now addresses the same human by two identifiers: a phone-number JID
// and a LID. Both are stored, because treating them as two people splits one
// real chat into two CRM conversations, which reads to an operator as data loss
// rather than as a bug.
type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`

	JID string `json:"jid"`
	LID string `json:"lid,omitempty"`
	// PhoneNumber is E.164 without a leading +. It is the CRM bridge, and the
	// reason this channel's contacts are first-class where Instagram's and
	// Telegram's are not.
	PhoneNumber string `json:"phoneNumber,omitempty"`
	// LeadID links to the CRM lead this contact is. Resolved on first inbound.
	LeadID *string `json:"leadId,omitempty"`

	Name             string     `json:"name,omitempty"`
	ContactName      string     `json:"contactName,omitempty"`
	VerifiedName     string     `json:"verifiedName,omitempty"`
	PictureURL       string     `json:"pictureUrl,omitempty"`
	IsBusiness       bool       `json:"isBusiness"`
	ProfileFetchedAt *time.Time `json:"profileFetchedAt,omitempty"`

	Blocked   bool       `json:"blocked"`
	BlockedAt *time.Time `json:"blockedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DisplayName prefers the name the operator's own phone knows, then the
// business-verified name, then the WhatsApp profile name, and only then the
// number. A row is never blank.
func (c *Contact) DisplayName() string {
	for _, candidate := range []string{c.ContactName, c.VerifiedName, c.Name} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	return c.Handle()
}

// Handle fills the CRM's "number" slot, which is what an operator searches by.
func (c *Contact) Handle() string {
	if c.PhoneNumber != "" {
		return "+" + c.PhoneNumber
	}
	return c.JID
}

// ProfileIsStale reports whether the cached profile should be refreshed.
// Enrichment is lazy because profile reads compete with the send budget.
func (c *Contact) ProfileIsStale(now time.Time, ttl time.Duration) bool {
	if c.ProfileFetchedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileFetchedAt) > ttl
}

// ---------------------------------------------------------------- conversation

// Conversation is the ENTRY, what the CRM treats as a conversation.
//
// One real WhatsApp chat is one conversation, always. This is a deliberate
// divergence from the Cloud API channel, where the entry is (campaign, lead) and
// the same person in two campaigns has two transcripts: an inbound webhook here
// carries no campaign, so it could not choose between them, and the operator
// would see one chat on their phone and two in the CRM.
type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`
	ContactID   string `json:"contactId"`

	ChatID  string `json:"chatId"`
	IsGroup bool   `json:"isGroup"`

	ConversationStatus string     `json:"conversationStatus,omitempty"`
	CloseSource        string     `json:"closeSource,omitempty"`
	CloseReason        string     `json:"closeReason,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	AutomationEnabled  *bool      `json:"automationEnabled,omitempty"`

	LastMessageAt         *time.Time `json:"lastMessageAt,omitempty"`
	LastCustomerMessageAt *time.Time `json:"lastCustomerMessageAt,omitempty"`
	LastAgentMessageAt    *time.Time `json:"lastAgentMessageAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RunsAutomation reports whether AI replies and workflows may act here.
//
// Groups are excluded by construction rather than by configuration: with a
// partial view of a group thread, an agent would be answering a conversation it
// cannot see.
func (c *Conversation) RunsAutomation(instanceHandlesGroups bool) bool {
	if c.IsGroup && !instanceHandlesGroups {
		return false
	}
	return c.AutomationEnabled == nil || *c.AutomationEnabled
}

// ---------------------------------------------------------------- phone

// NormalizePhone reduces a number to digits only.
//
// WhatsApp JIDs, the provider's `number` field and our leads table all disagree
// about the leading +, and comparing an un-normalized pair is how a lead ends up
// duplicated. Everything in this channel stores and compares digits.
func NormalizePhone(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// PhoneFromJID extracts the phone number from a JID.
//
// A LID-form JID ("…@lid") carries no phone number at all, and returning its
// numeric part would invent one: that value is an opaque identifier, and
// matching a lead against it would attach a conversation to the wrong person.
func PhoneFromJID(jid string) string {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return ""
	}
	user, domain, found := strings.Cut(jid, "@")
	if found && strings.EqualFold(domain, DomainLID) {
		return ""
	}
	// Multi-device JIDs carry a device suffix ("5511999999999:12@…").
	user, _, _ = strings.Cut(user, ":")
	return NormalizePhone(user)
}

// JID domains, as they appear in the provider's chat ids.
const (
	DomainUser       = "s.whatsapp.net"
	DomainLID        = "lid"
	DomainGroup      = "g.us"
	DomainNewsletter = "newsletter"
)

// IsGroupJID reports whether a chat id addresses a group.
func IsGroupJID(jid string) bool {
	return strings.HasSuffix(strings.TrimSpace(jid), "@"+DomainGroup)
}

// IsNewsletterJID reports whether a chat id addresses a channel/newsletter.
// Those are a publishing surface, not an attendance surface, and are ignored.
func IsNewsletterJID(jid string) bool {
	return strings.HasSuffix(strings.TrimSpace(jid), "@"+DomainNewsletter)
}

// UserJID renders the addressable JID for an E.164 number.
func UserJID(phone string) string {
	digits := NormalizePhone(phone)
	if digits == "" {
		return ""
	}
	return digits + "@" + DomainUser
}
