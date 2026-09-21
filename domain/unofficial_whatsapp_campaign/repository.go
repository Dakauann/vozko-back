package unofficial_whatsapp_campaign

import (
	"time"

	"vozko/domain/campaign"
	"vozko/domain/shared"
)

var QueueNamespace = campaign.Namespace{
	Topic: "unofficial_whatsapp_campaign_dispatch",
	Key:   "campaign:unofficial_whatsapp",
}

type ListCampaignsInput struct {
	WorkspaceID   string
	DepartmentIDs []string
	InstanceIDs   []string
	Search        string
	Status        campaign.Status
	Archived      *bool
	Options       shared.QueryOptions
}

type ListEntriesInput struct {
	CampaignID string
	Status     campaign.SendStatus
	Number     string
	Search     string
	StageID    string
	ErrorCode  int
	Options    shared.QueryOptions
}

type WorkspaceSummaryFilter struct {
	WorkspaceID   string
	DepartmentIDs []string
	InstanceIDs   []string
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

type Repository interface {
	Create(c *Campaign) error
	Update(campaignID string, c *Campaign) error
	Delete(campaignID string) error
	FindByID(campaignID string) (*Campaign, error)
	List(input ListCampaignsInput) (*shared.PaginatedResult[*Campaign], error)
	ListByStatus(status campaign.Status) ([]*Campaign, error)
	ListRunningByInstance(instanceID string) ([]*Campaign, error)
	ListScheduledToStart(at time.Time, limit int) ([]*Campaign, error)
	UpdateStatus(campaignID string, status campaign.Status, allowed ...campaign.Status) (bool, error)
	UpdateStatusReason(campaignID, reason string) error
	UpdateResetCode(campaignID, code string) error
	UpdateClearCode(campaignID, code string) error
}

type RecordSendInput struct {
	ContactID         string
	ConversationID    string
	ProviderMessageID string
	MessageID         string
	VariantIndex      int
	SentAt            time.Time
}

type EntryRepository interface {
	CreateMany(entries []Entry) ([]Entry, error)
	FindByID(entryID string) (*Entry, error)
	FindByProviderMessageID(providerMessageID string) (*Entry, error)
	FindByCampaignAndLead(campaignID, leadID string) (*Entry, error)
	FindLatestByConversationID(conversationID string) (*Entry, error)
	Delete(entryID string) error
	DeleteByCampaignID(campaignID string) error

	List(input ListEntriesInput) (*shared.PaginatedResult[*EntryWithLead], error)
	ListByStatus(campaignID string, status campaign.SendStatus, limit int) ([]Entry, error)
	ListRecentlyUpdated(campaignID string, limit int) ([]Entry, error)

	CountByStatus(campaignID string) (*campaign.Counts, error)
	CountByStatusForCampaigns(campaignIDs []string) (map[string]*campaign.Counts, error)

	UpdateStatus(entryID string, status campaign.SendStatus, providerMessageID string, errorCode int, errorMessage string) error
	UpdateStatusByProviderMessageID(providerMessageID string, status campaign.SendStatus) error
	RecordCheck(entryID, jid string, at time.Time, onWhatsApp bool) error
	RecordSend(entryID string, in RecordSendInput) error

	UpdateEntryDetails(entryID string, in UpdateEntryDetails) error

	ResetAllStatuses(campaignID string) (int64, error)
	UpsertEntries(campaignID string, entries []Entry) error

	ConversationIDsForCampaign(campaignID string) ([]string, error)
}

type UpdateEntryDetails struct {
	LeadID    string
	Number    string
	Name      string
	Variables []string
	Metadata  map[string]interface{}
}

type SummaryAggregator interface {
	CountByStatusForWorkspace(filter WorkspaceSummaryFilter) (*campaign.Counts, error)
}
