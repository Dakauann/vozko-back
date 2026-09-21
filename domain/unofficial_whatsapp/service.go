package unofficial_whatsapp

import (
	"context"
	"errors"
	"strings"
	"time"
)

type ServerRef struct {
	BaseURL    string
	AdminToken string
}

type InstanceRef struct {
	BaseURL string
	Token   string
}

func RefFor(server *Server, instance *Instance) InstanceRef {
	if server == nil || instance == nil {
		return InstanceRef{}
	}
	return InstanceRef{BaseURL: server.BaseURL, Token: instance.InstanceToken}
}

type CreatedInstance struct {
	ProviderInstanceID string
	Token              string
	Name               string
}

type CreateInstanceInput struct {
	Name          string
	WorkspaceID   string
	OurInstanceID string
}

type ConnectMode string

const (
	ConnectModeQR      ConnectMode = "qr"
	ConnectModePairing ConnectMode = "pairing"
)

func (m ConnectMode) Valid() bool { return m == ConnectModeQR || m == ConnectModePairing }

const (
	QRCodeTTL      = 2 * time.Minute
	PairingCodeTTL = 5 * time.Minute
)

type ConnectInput struct {
	Mode       ConnectMode
	Phone      string
	SystemName string
}

type Session struct {
	State     string
	Connected bool
	LoggedIn  bool

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
	if connected {
		return StatusConnected, true
	}
	return "", false
}

type InstanceAPI interface {
	CreateInstance(ctx context.Context, server ServerRef, in CreateInstanceInput) (*CreatedInstance, error)
	ListInstances(ctx context.Context, server ServerRef) ([]RemoteInstance, error)

	Connect(ctx context.Context, ref InstanceRef, in ConnectInput) (*Session, error)
	Status(ctx context.Context, ref InstanceRef) (*Session, error)
	Disconnect(ctx context.Context, ref InstanceRef) error
	Reset(ctx context.Context, ref InstanceRef) error
	DeleteInstance(ctx context.Context, ref InstanceRef) error
}

type RemoteInstance struct {
	ProviderInstanceID string
	Name               string
	State              string
	WorkspaceID        string
	OurInstanceID      string
}

type WebhookSubscription struct {
	URL             string
	Enabled         bool
	Events          []string
	ExcludeMessages []string
}

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
		"groups",
	}
}

type WebhookDeliveryError struct {
	At         time.Time
	URL        string
	Event      string
	StatusCode int
	Attempts   int
	Error      string
}

type WebhookAPI interface {
	SetWebhook(ctx context.Context, ref InstanceRef, sub WebhookSubscription) error
	GetWebhooks(ctx context.Context, ref InstanceRef) ([]WebhookSubscription, error)
	WebhookErrors(ctx context.Context, ref InstanceRef) ([]WebhookDeliveryError, error)
}

type DiagnosticsAPI interface {
	MessagingLimits(ctx context.Context, ref InstanceRef) (*Restriction, error)
	DisableBuiltInChatbot(ctx context.Context, ref InstanceRef) error
}

type UpdateParticipantsInput struct {
	GroupJID     string
	Action       GroupAction
	Participants []string
}

type GroupAPI interface {
	GroupInfo(ctx context.Context, ref InstanceRef, groupJID string, opts GroupInfoOptions) (*Group, error)
	ListGroups(ctx context.Context, ref InstanceRef, withParticipants bool) ([]*Group, error)

	UpdateGroupName(ctx context.Context, ref InstanceRef, groupJID, name string) error
	UpdateGroupDescription(ctx context.Context, ref InstanceRef, groupJID, description string) error
	UpdateGroupImage(ctx context.Context, ref InstanceRef, groupJID, image string) error
	UpdateParticipants(ctx context.Context, ref InstanceRef, in UpdateParticipantsInput) error
	UpdateAnnounce(ctx context.Context, ref InstanceRef, groupJID string, adminsOnly bool) error
	UpdateLocked(ctx context.Context, ref InstanceRef, groupJID string, adminsOnly bool) error
	LeaveGroup(ctx context.Context, ref InstanceRef, groupJID string) error
}

type GroupInfoOptions struct {
	WithInviteLink bool
	Force          bool
}

type ProviderAPI interface {
	InstanceAPI
	WebhookAPI
	DiagnosticsAPI
}

type ProviderError struct {
	HTTPStatus       int
	Message          string
	ErrorSource      string
	ProviderCode     int
	ErrorKey         string
	LocalizedMessage string
	Restriction      *Restriction
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

const whatsAppRestrictionCode = 463

func (e *ProviderError) IsRestriction() bool {
	if e == nil {
		return false
	}
	return e.ProviderCode == whatsAppRestrictionCode || e.Restriction != nil
}

func (e *ProviderError) Retryable() bool {
	if e == nil || e.IsRestriction() {
		return false
	}
	return e.HTTPStatus == 429 || e.HTTPStatus == 503 || e.HTTPStatus >= 500
}

func (e *ProviderError) NeedsReconnect() bool {
	return e != nil && e.HTTPStatus == 401
}

func (e *ProviderError) AtCapacity() bool {
	return e != nil && (e.HTTPStatus == 429 || e.HTTPStatus == 503)
}

func AsProviderError(err error) (*ProviderError, bool) {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return provErr, true
	}
	return nil, false
}
