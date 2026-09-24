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

const MaxTextRunes = 65536

const (
	MaxImageBytes    int64 = 16 << 20
	MaxVideoBytes    int64 = 16 << 20
	MaxAudioBytes    int64 = 16 << 20
	MaxDocumentBytes int64 = 100 << 20
)

const (
	MaxButtonOptions      = 3
	MaxListOptions        = 10
	MaxOptionLabelRunes   = 24
	MaxOptionPayloadBytes = 256
)

const (
	DefaultSendDelayMinMS = 3000
	DefaultSendDelayMaxMS = 12000
	MinSendDelayMS        = 500
)

const ProviderUazapi = "uazapi"

type Server struct {
	ID          string  `json:"id"`
	WorkspaceID *string `json:"workspaceId,omitempty"`

	Name       string `json:"name"`
	Provider   string `json:"provider"`
	BaseURL    string `json:"baseUrl"`
	AdminToken string `json:"-"`

	Capacity int  `json:"capacity"`
	InUse    int  `json:"inUse"`
	Enabled  bool `json:"enabled"`
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

func (s *Server) HasCapacity() bool {
	return s.Enabled && !s.Draining && s.Capacity > 0 && s.InUse < s.Capacity
}

type Status string

const (
	StatusProvisioning    Status = "PROVISIONING"
	StatusAwaitingScan    Status = "AWAITING_SCAN"
	StatusConnected       Status = "CONNECTED"
	StatusHibernated      Status = "HIBERNATED"
	StatusDisconnected    Status = "DISCONNECTED"
	StatusBanned          Status = "BANNED"
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
		return next == StatusConnected || next == StatusDisconnected ||
			next == StatusProvisionFailed || next == StatusBanned
	case StatusConnected:
		return next == StatusHibernated || next == StatusDisconnected || next == StatusBanned
	case StatusHibernated:
		return next == StatusConnected || next == StatusDisconnected || next == StatusBanned
	case StatusDisconnected:
		return next == StatusAwaitingScan || next == StatusConnected || next == StatusBanned
	case StatusProvisionFailed:
		return next == StatusProvisioning
	case StatusBanned:
		return false
	}
	return false
}

func (s Status) Terminal() bool { return s == StatusBanned }

type Restriction struct {
	CanSendNewChats *bool      `json:"canSendNewChats,omitempty"`
	Key             string     `json:"key,omitempty"`
	Message         string     `json:"message,omitempty"`
	Until           *time.Time `json:"until,omitempty"`
	UsedQuota       int        `json:"usedQuota,omitempty"`
	TotalQuota      int        `json:"totalQuota,omitempty"`
	CheckedAt       *time.Time `json:"checkedAt,omitempty"`
}

func (r Restriction) Active(now time.Time) bool {
	if r.Until != nil && now.Before(*r.Until) {
		return true
	}
	return r.CanSendNewChats != nil && !*r.CanSendNewChats
}

type Instance struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`
	ServerID     string  `json:"serverId"`
	Provider     string  `json:"provider"`

	ProviderInstanceID string `json:"providerInstanceId"`
	ProviderName       string `json:"providerName,omitempty"`
	InstanceToken      string `json:"-"`

	DeliveryToken     string     `json:"-"`
	DeliveryTokenHash string     `json:"-"`
	WebhookSetAt      *time.Time `json:"webhookSetAt,omitempty"`

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
	LastPolledAt      *time.Time `json:"lastPolledAt,omitempty"`

	Restriction Restriction `json:"restriction"`

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
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
	HandleGroups         bool    `json:"handleGroups"`

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

func (i *Instance) SessionLive() bool {
	return i.Status == StatusConnected
}

func (i *Instance) CanSend(now time.Time) (bool, error) {
	if !i.SessionLive() {
		return false, ErrInstanceNotConnected
	}
	if i.Restriction.Active(now) {
		return false, ErrRestrictedByWA
	}
	return true, nil
}

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

type Contact struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`

	JID         string  `json:"jid"`
	LID         string  `json:"lid,omitempty"`
	IsGroup     bool    `json:"isGroup"`
	PhoneNumber string  `json:"phoneNumber,omitempty"`
	LeadID      *string `json:"leadId,omitempty"`

	Name             string     `json:"name,omitempty"`
	ContactName      string     `json:"contactName,omitempty"`
	VerifiedName     string     `json:"verifiedName,omitempty"`
	PictureURL       string     `json:"pictureUrl,omitempty"`
	PictureSourceURL string     `json:"-"`
	IsBusiness       bool       `json:"isBusiness"`
	ProfileFetchedAt *time.Time `json:"profileFetchedAt,omitempty"`

	Blocked   bool       `json:"blocked"`
	BlockedAt *time.Time `json:"blockedAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Contact) DisplayName() string {
	for _, candidate := range []string{c.ContactName, c.VerifiedName, c.Name} {
		if v := strings.TrimSpace(candidate); v != "" {
			return v
		}
	}
	if c.IsGroup {
		return UnnamedGroupLabel
	}
	return c.Handle()
}

func (c *Contact) Handle() string {
	if c.IsGroup {
		return ""
	}
	if c.PhoneNumber != "" {
		return "+" + c.PhoneNumber
	}
	return c.JID
}

func (c *Contact) ProfileRef() string {
	if !c.IsGroup && c.PhoneNumber != "" {
		return c.PhoneNumber
	}
	return c.JID
}

func (c *Contact) ProfileIsStale(now time.Time, ttl time.Duration) bool {
	if c.ProfileFetchedAt == nil {
		return true
	}
	return now.Sub(*c.ProfileFetchedAt) > ttl
}

type Conversation struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	InstanceID  string `json:"instanceId"`
	ContactID   string `json:"contactId"`

	ChatID  string `json:"chatId"`
	IsGroup bool   `json:"isGroup"`
	// CampaignID is the campaign this conversation was opened for; empty for a
	// chat's campaign-less conversation.
	CampaignID string `json:"campaignId,omitempty"`

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

func (c *Conversation) InScope(instanceHandlesGroups bool) bool {
	return !c.IsGroup || instanceHandlesGroups
}

func (c *Conversation) RunsAutomation(instanceHandlesGroups bool) bool {
	if !c.InScope(instanceHandlesGroups) {
		return false
	}
	return c.AutomationEnabled == nil || *c.AutomationEnabled
}

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

func PhoneFromJID(jid string) string {
	jid = strings.TrimSpace(jid)
	if jid == "" {
		return ""
	}
	user, domain, found := strings.Cut(jid, "@")
	if found && !strings.EqualFold(domain, DomainUser) {
		return ""
	}
	user, _, _ = strings.Cut(user, ":")
	return NormalizePhone(user)
}

const (
	DomainUser       = "s.whatsapp.net"
	DomainLID        = "lid"
	DomainGroup      = "g.us"
	DomainNewsletter = "newsletter"
)

func IsGroupJID(jid string) bool {
	return strings.HasSuffix(strings.TrimSpace(jid), "@"+DomainGroup)
}

func IsNewsletterJID(jid string) bool {
	return strings.HasSuffix(strings.TrimSpace(jid), "@"+DomainNewsletter)
}

func UserJID(phone string) string {
	digits := NormalizePhone(phone)
	if digits == "" {
		return ""
	}
	return digits + "@" + DomainUser
}
