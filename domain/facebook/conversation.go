package facebook

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrContactNotFound      = errors.New("facebook contact not found")
	ErrConversationNotFound = errors.New("facebook conversation not found")
)

const (
	PageInboxAppID       = "263902037430900"
	PageInboxAppIDLegacy = "26390203743090"
	OtherAppOwner        = "other"
)

func IsPageInboxApp(appID string) bool {
	return appID == PageInboxAppID || appID == PageInboxAppIDLegacy
}

type ProfileStatus string

const (
	ProfileUnknown   ProfileStatus = "UNKNOWN"
	ProfileAvailable ProfileStatus = "AVAILABLE"
	ProfileDenied    ProfileStatus = "DENIED"
	ProfileNone      ProfileStatus = "NO_PROFILE"
)

const (
	profileRefreshAfter  = 7 * 24 * time.Hour
	profileRetryDeniedIn = 30 * 24 * time.Hour
)

type Contact struct {
	ID          string
	WorkspaceID string
	PageID      string
	PSID        string

	Name              string
	FirstName         string
	LastName          string
	AvatarStorageKey  string
	ProfileStatus     ProfileStatus
	ProfileFetchedAt  *time.Time
	Unreachable       bool
	UnreachableReason string

	LeadID  *string
	Blocked bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c *Contact) DisplayName() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	if n := strings.TrimSpace(strings.TrimSpace(c.FirstName) + " " + strings.TrimSpace(c.LastName)); n != "" {
		return n
	}
	psid := c.PSID
	if len(psid) > 4 {
		psid = psid[len(psid)-4:]
	}
	return "Facebook user " + psid
}

func (c *Contact) ProfileIsStale(now time.Time) bool {
	if c.ProfileFetchedAt == nil {
		return true
	}
	age := now.Sub(*c.ProfileFetchedAt)
	switch c.ProfileStatus {
	case ProfileDenied, ProfileNone:
		return age > profileRetryDeniedIn
	}
	return age > profileRefreshAfter
}

type ContactProfile struct {
	Name             string
	FirstName        string
	LastName         string
	AvatarStorageKey string
	Status           ProfileStatus
	FetchedAt        time.Time
}

type WatermarkKind string

const (
	WatermarkDelivered WatermarkKind = "delivered"
	WatermarkRead      WatermarkKind = "read"
)

type Conversation struct {
	ID          string
	WorkspaceID string
	PageID      string
	ContactID   string

	FBConversationID *string

	ConversationStatus string
	CloseSource        string
	CloseReason        string
	ClosedAt           *time.Time
	AutomationEnabled  *bool

	LastMessageAt         *time.Time
	LastCustomerMessageAt *time.Time
	LastAgentMessageAt    *time.Time

	DeliveredWatermark *time.Time
	ReadWatermark      *time.Time

	ThreadOwnerAppID  string
	ThreadOwnerSeenAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c *Conversation) LastInboundAt() *time.Time { return c.LastCustomerMessageAt }

func (c *Conversation) WatermarkAdvances(kind WatermarkKind, at time.Time) bool {
	current := c.DeliveredWatermark
	if kind == WatermarkRead {
		current = c.ReadWatermark
	}
	return current == nil || at.After(*current)
}

func (c *Conversation) OwnedByAnotherApp(ourAppID string) bool {
	owner := strings.TrimSpace(c.ThreadOwnerAppID)
	return owner != "" && owner != ourAppID
}
