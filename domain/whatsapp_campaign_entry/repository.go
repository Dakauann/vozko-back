package whatsapp_campaign_entry

import (
	"time"

	"vozko/domain/shared"
)

type Repository interface {
	Create(entry *WhatsAppCampaignEntry) error

	CreateMany(entries []WhatsAppCampaignEntry) ([]WhatsAppCampaignEntry, error)

	FindByID(id string) (*WhatsAppCampaignEntry, error)
	FindByMessageID(messageID string) (*WhatsAppCampaignEntry, error)

	FindByIDs(ids []string) ([]*WhatsAppCampaignEntry, error)

	FindByCampaignAndLead(campaignID, leadID string) (*WhatsAppCampaignEntry, error)

	FindByCampaignAndNumber(campaignID, number string) (*WhatsAppCampaignEntry, error)

	Delete(id string) error

	DeleteByCampaignID(campaignID string) error

	List(input ListEntriesInput) (*shared.PaginatedResult[*WhatsAppCampaignEntry], error)

	ListByCampaignID(campaignID string) ([]WhatsAppCampaignEntry, error)

	ListByLeadID(leadID string) ([]WhatsAppCampaignEntry, error)

	ListRecentlyUpdated(campaignID string, limit int) ([]WhatsAppCampaignEntry, error)

	CountByCampaignID(campaignID string) (int64, error)

	CountByStatus(campaignID string) (*StatusCounts, error)

	// CountByStatusForCampaigns aggregates per-status entry counts for many
	// campaigns in a single query, keyed by campaign ID. Campaigns with no
	// entries are simply absent from the returned map. This exists so list
	// endpoints can build per-campaign metrics without an N+1 of CountByStatus.
	CountByStatusForCampaigns(campaignIDs []string) (map[string]*StatusCounts, error)

	UpdateStatus(entryID string, status SendStatus, messageID string, errorCode int, errorMessage string) error

	UpdateReceivedBusinessPhone(entryID string, businessPhoneID string) error

	UpdateStatusByMessageID(messageID string, status SendStatus) error

	UpdateStatusByNumber(campaignID, number string, status SendStatus, messageID string) error

	ResetAllStatuses(campaignID string) (int64, error)

	ReplaceCampaignEntries(campaignID string, entries []WhatsAppCampaignEntry) error

	UpsertCampaignEntries(campaignID string, entries []WhatsAppCampaignEntry) error

	ListByStatus(campaignID string, status SendStatus, limit int) ([]WhatsAppCampaignEntry, error)

	ListEntriesWithLeads(input ListEntriesInput) (*shared.PaginatedResult[*EntryWithLead], error)

	ListEntriesWithLeadsForUser(input ListEntriesForUserInput) ([]*EntryWithLead, int64, error)

	CanUserAccessEntry(workspaceID, entryID string, isAdmin bool) (bool, error)

	GetAccessibleEntryIDs(workspaceID string, isAdmin bool) ([]string, error)

	GetEntryIDsByCampaign(campaignID string) ([]string, error)

	FindByNumber(number string) (*WhatsAppCampaignEntry, error)

	FindByNumberAndBusinessPhone(number string, businessPhoneID string) (*WhatsAppCampaignEntry, error)

	GetCampaignForEntry(entryID string) (*EntryCampaignInfo, error)

	UpdateAutomationEnabled(entryID string, enabled *bool) error

	UpdateMetadata(entryID string, metadata map[string]interface{}) error

	// UpdateConversationStatus applies lifecycle fields in one UPDATE (no N+1).
	// write.SetCloseMeta stamps close_* ; write.ClearCloseMeta nulls them.
	UpdateConversationStatus(entryID string, write ConversationStatusWrite) error

	// ListEligibleForAutoClose returns open conversations past workspace idle
	// policy in a single JOIN (entries + campaigns + workspace_configs).
	// Ordered by last_agent_message_at ASC, hard-capped by limit.
	ListEligibleForAutoClose(limit int) ([]AutoCloseCandidate, error)

	// ListEligibleForMaxAge returns open conversations with last_message_at
	// older than workspace max-age (any side). Separate from customer_idle.
	ListEligibleForMaxAge(limit int) ([]AutoCloseCandidate, error)

	CountByConversationStatus(campaignID string) (map[string]int64, error)

	CountByConversationStatusForWorkspace(workspaceID string) (map[string]int64, error)
}

// ConversationStatusWrite is the atomic status + close-meta patch for entries.
type ConversationStatusWrite struct {
	Status         string
	SetCloseMeta   bool
	CloseSource    string
	CloseReason    string
	ClosedAt       *time.Time
	ClearCloseMeta bool
}

// AutoCloseCandidate is one WhatsApp entry eligible for system idle finish.
type AutoCloseCandidate struct {
	EntryID            string
	WorkspaceID        string
	LastAgentMessageAt time.Time
}

type EntryCampaignInfo struct {
	CampaignID      string
	WorkspaceID     string
	BusinessPhoneID string
}

type ListEntriesForUserInput struct {
	UserID   string
	Page     int
	PageSize int
	Search   string
}

type ListEntriesInput struct {
	CampaignID         string
	LeadID             string
	Status             SendStatus
	Number             string
	BusinessPhoneID    string
	StageID            string
	ErrorCode          int
	ConversationFilter ConversationFilter
	Options            shared.QueryOptions
}

type ConversationFilter struct {
	HasToolCalls *bool

	ToolName string

	MessageType string

	MinMessageCount *int

	MaxMessageCount *int
}

func (f ConversationFilter) HasAnyFilter() bool {
	return f.HasToolCalls != nil ||
		f.ToolName != "" ||
		f.MessageType != "" ||
		f.MinMessageCount != nil ||
		f.MaxMessageCount != nil
}

type EntryWithLead struct {
	Entry                    *WhatsAppCampaignEntry `json:"entry"`
	LeadID                   string                 `json:"leadId"`
	Number                   string                 `json:"number"`
	Name                     string                 `json:"name,omitempty"`
	Age                      *int                   `json:"age,omitempty"`
	Metadata                 interface{}            `json:"metadata,omitempty"`
	LastMessageAt            *time.Time             `json:"lastMessageAt,omitempty"`
	CanReceiveNormalMessages bool                   `json:"canReceiveNormalMessages"`
}
