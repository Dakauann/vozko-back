package lead

import "time"

import "vozko/domain/shared"

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

	ResolveCampaignNames(wcIDs []string) map[string]string
}

type BulkLeadInput struct {
	Number string
	Name   string
	Age    *int
}

type ListLeadsInput struct {
	WorkspaceID         string
	Number              string
	Name                string
	CreatedFrom         *time.Time
	CreatedTo           *time.Time
	AgeFrom             *int
	AgeTo               *int
	HasWhatsAppCampaign *bool
	Options             shared.QueryOptions
}

type LeadSummary struct {
	WhatsAppCampaigns  int        `json:"whatsappCampaigns"`
	TotalCampaigns     int        `json:"totalCampaigns"`
	LastActivityAt     *time.Time `json:"lastActivityAt,omitempty"`
	WhatsAppWindowOpen bool       `json:"whatsappWindowOpen"`
	WindowExpiresAt    *time.Time `json:"windowExpiresAt,omitempty"`
}

type LeadWithSummary struct {
	Lead    *Lead
	Summary *LeadSummary
}
