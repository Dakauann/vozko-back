package lead

import (
	"strings"
	"time"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

type Repository interface {
	Create(lead *Lead) error

	FindByID(workspaceID, id string) (*Lead, error)

	FindByIDs(workspaceID string, ids []string) ([]*Lead, error)

	FindByNumber(workspaceID, number string) (*Lead, error)

	FindOrCreate(workspaceID, number string, update LeadUpdate) (*Lead, bool, error)

	FindOrCreateMany(workspaceID string, inputs []BulkLeadInput) (map[string]*Lead, error)

	ImportMany(workspaceID string, inputs []BulkLeadInput, policy ExistingPolicy) (*ImportOutcome, error)

	Update(workspaceID, id string, update LeadUpdate) error

	Rename(workspaceID, id, name string) error

	Delete(workspaceID, id string) error

	List(input ListLeadsInput) (*shared.PaginatedResult[*Lead], error)

	ListWithSummary(input ListLeadsInput) (*shared.PaginatedResult[*LeadWithSummary], error)

	Facets(input ListLeadsInput) (*LeadFacets, error)

	ResolveCampaignNames(wcIDs []string) map[string]string
}

type BulkLeadInput struct {
	Number string
	Name   string
	Age    *int
}

type ListLeadsInput struct {
	WorkspaceID string
	Filter      crmfilter.Filter
	Options     shared.QueryOptions
}

type SortKey string

const (
	SortCreatedAt      SortKey = "createdAt"
	SortUpdatedAt      SortKey = "updatedAt"
	SortName           SortKey = "name"
	SortNumber         SortKey = "number"
	SortAge            SortKey = "age"
	SortLastActivityAt SortKey = "lastActivityAt"
	SortCampaigns      SortKey = "campaigns"
	SortMemories       SortKey = "memories"
	SortLastMemoryAt   SortKey = "lastMemoryAt"
)

var DefaultSort = shared.Sort{Field: string(SortCreatedAt), Direction: shared.SortDesc}

func AllSortKeys() []SortKey {
	return []SortKey{
		SortCreatedAt, SortUpdatedAt, SortLastActivityAt,
		SortName, SortNumber, SortAge,
		SortCampaigns, SortMemories, SortLastMemoryAt,
	}
}

func ParseSortKey(value string) (SortKey, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, key := range AllSortKeys() {
		if strings.ToLower(string(key)) == normalized {
			return key, true
		}
	}
	return "", false
}

func (k SortKey) Valid() bool {
	_, ok := ParseSortKey(string(k))
	return ok
}

type LeadFacets struct {
	Total           int64 `json:"total"`
	Blocked         int64 `json:"blocked"`
	Active          int64 `json:"active"`
	WindowOpen      int64 `json:"windowOpen"`
	WindowClosed    int64 `json:"windowClosed"`
	WithCampaign    int64 `json:"withCampaign"`
	WithoutCampaign int64 `json:"withoutCampaign"`
	WithMemory      int64 `json:"withMemory"`
	WithoutMemory   int64 `json:"withoutMemory"`
	Named           int64 `json:"named"`
	Unnamed         int64 `json:"unnamed"`

	MemoryCategories map[string]int64 `json:"memoryCategories"`
	Channels         map[string]int64 `json:"channels"`
	CampaignStatuses map[string]int64 `json:"campaignStatuses"`
}

type LeadSummary struct {
	WhatsAppCampaigns  int        `json:"whatsappCampaigns"`
	TotalCampaigns     int        `json:"totalCampaigns"`
	LastActivityAt     *time.Time `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool       `json:"whatsappWindowOpen"`
	WindowExpiresAt    *time.Time `json:"windowExpiresAt,omitempty"`
	Memories           int        `json:"memories"`
	LastMemoryAt       *time.Time `json:"lastMemoryAt,omitempty"`
}

type LeadWithSummary struct {
	Lead    *Lead
	Summary *LeadSummary
}
