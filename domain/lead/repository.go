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

	Update(workspaceID, id string, update LeadUpdate) error

	Delete(workspaceID, id string) error

	List(input ListLeadsInput) (*shared.PaginatedResult[*Lead], error)

	ListWithSummary(input ListLeadsInput) (*shared.PaginatedResult[*LeadWithSummary], error)

	// Facets counts the same filtered set the list returns, broken down by the
	// dimensions the filter UI offers. It exists so the counts beside each
	// filter option come from the query that produced the rows, not from the
	// current page (which is how a "3 bloqueados" badge ends up meaning "3 on
	// this page of 20").
	Facets(input ListLeadsInput) (*LeadFacets, error)

	ResolveCampaignNames(wcIDs []string) map[string]string
}

type BulkLeadInput struct {
	Number string
	Name   string
	Age    *int
}

// ListLeadsInput is the whole read query: who is asking, what to keep, how to
// order and page it.
//
// Filter is the reusable crmfilter expression compiled by the lead descriptor.
// There is deliberately no second set of scalar filter fields beside it: the
// legacy `?number=&name=&ageFrom=` query params are translated into predicates
// at the HTTP edge, so one filter model reaches the database no matter which
// client shape asked for it.
type ListLeadsInput struct {
	WorkspaceID string
	Filter      crmfilter.Filter
	Options     shared.QueryOptions
}

// SortKey is a stable, client-facing ordering key. The repository owns the
// mapping from key to SQL, so no layer above it ever names a column — which is
// what lets `lastActivity` be a five-table GREATEST() without the HTTP handler
// knowing.
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

// DefaultSort is what an unsorted request gets: newest leads first.
var DefaultSort = shared.Sort{Field: string(SortCreatedAt), Direction: shared.SortDesc}

// AllSortKeys lists every valid key, in the order a UI should offer them.
func AllSortKeys() []SortKey {
	return []SortKey{
		SortCreatedAt, SortUpdatedAt, SortLastActivityAt,
		SortName, SortNumber, SortAge,
		SortCampaigns, SortMemories, SortLastMemoryAt,
	}
}

// ParseSortKey resolves a case-insensitive client value to a known key.
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

// LeadFacets are the aggregate counts over a filtered lead set. Zero-valued
// buckets are still reported so a filter option can render "0" instead of
// disappearing, which is the difference between "none match" and "we forgot to
// count".
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

	// Keyed breakdowns. Each counts DISTINCT leads, never rows: a lead with
	// four `deal` memories is one lead under "deal".
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
