package unofficial_whatsapp

import (
	"context"
	"time"
	"vozko/domain/conversation"

	"vozko/domain/shared"
)

type ServerRepository interface {
	Create(ctx context.Context, s *Server) error
	Update(ctx context.Context, s *Server) error
	FindByID(ctx context.Context, id string) (*Server, error)
	FindByBaseURL(ctx context.Context, baseURL string) (*Server, error)

	ListPlacementCandidates(ctx context.Context, workspaceID string) ([]*Server, error)
	ListAll(ctx context.Context) ([]*Server, error)

	ClaimCapacity(ctx context.Context, serverID string) (claimed bool, err error)
	ReleaseCapacity(ctx context.Context, serverID string) error
	SyncCapacity(ctx context.Context, serverID string, inUse int) error
	RecordHealth(ctx context.Context, serverID string, healthyAt *time.Time, lastError string) error
}

type InstanceRepository interface {
	Create(ctx context.Context, i *Instance) error
	Update(ctx context.Context, i *Instance) error

	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateSession(ctx context.Context, id string, in SessionUpdate) error
	UpdateRestriction(ctx context.Context, id string, r Restriction) error
	SetWebhookRegistered(ctx context.Context, id string, at time.Time) error
	RotateDeliveryToken(ctx context.Context, id, token, tokenHash string) error

	FindByID(ctx context.Context, id string) (*Instance, error)
	FindByDeliveryTokenHash(ctx context.Context, tokenHash string) (*Instance, error)
	FindByJID(ctx context.Context, jid string) (*Instance, error)
	FindByProviderInstanceID(ctx context.Context, serverID, providerInstanceID string) (*Instance, error)

	ListByWorkspace(ctx context.Context, in ListInstancesInput) (*shared.PaginatedResult[*Instance], error)
	ListByServer(ctx context.Context, serverID string) ([]*Instance, error)
	ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*Instance, error)
	ListConnected(ctx context.Context, limit int) ([]*Instance, error)
	CountByServer(ctx context.Context, serverID string) (int, error)
	CountByWorkspace(ctx context.Context, workspaceID string) (int, error)

	Delete(ctx context.Context, id string) error
}

type SessionUpdate struct {
	Status               *Status
	StatusReason         string
	JID                  string
	LID                  string
	PhoneNumber          string
	ProfileName          string
	ProfilePicURL        string
	IsBusinessAcct       bool
	Platform             string
	ConnectedAt          *time.Time
	LastDisconnectAt     *time.Time
	LastDisconnectReason string
	PolledAt             time.Time
}

type ListInstancesInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Scope       DepartmentScope
	Options     shared.QueryOptions
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByJID(ctx context.Context, instanceID, jid string) (*Contact, error)
	FindByHandles(ctx context.Context, instanceID string, handles []string) ([]*Contact, error)

	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error
	LinkLead(ctx context.Context, id, leadID string) error
}

type FindOrCreateContactInput struct {
	WorkspaceID string
	InstanceID  string
	JID         string
	LID         string
	PhoneNumber string
	Name        string
	IsGroup     bool
}

type ContactProfile struct {
	Name             string
	ContactName      string
	VerifiedName     string
	PictureURL       string
	PictureSourceURL string
	IsBusiness       bool
	FetchedAt        time.Time
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByChatID(ctx context.Context, instanceID, chatID string) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	CampaignIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)

	CountByStatus(ctx context.Context, workspaceID, instanceID string) (map[string]int64, error)
}

type FindOrCreateConversationInput struct {
	WorkspaceID string
	InstanceID  string
	ContactID   string
	ChatID      string
	IsGroup     bool
}

type GroupRepository interface {
	Upsert(ctx context.Context, g *Group) error

	FindByJID(ctx context.Context, instanceID, jid string) (*Group, error)
	FindByID(ctx context.Context, id string) (*Group, error)
	ListByInstance(ctx context.Context, instanceID string) ([]*Group, error)
	Participants(ctx context.Context, groupID string) ([]GroupParticipant, error)

	MarkStale(ctx context.Context, instanceID, jid string, at time.Time) error

	LinkParticipantContacts(ctx context.Context, groupID, instanceID string) error

	Delete(ctx context.Context, id string) error
}

type ProcessedEventRepository interface {
	Claim(ctx context.Context, key, channel, instanceID string) (claimed bool, err error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
