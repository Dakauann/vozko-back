package whatsapp_campaign

import (
	"time"

	"vozko/domain/shared"
)

type Repository interface {
	Create(campaign *Campaign) error
	Update(campaignID string, campaign *Campaign) error
	Delete(campaignID string) error
	FindByID(campaignID string) (*Campaign, error)
	FindLatestOrganicByBusinessPhone(workspaceID string, businessPhoneID string) (*Campaign, error)
	List(input ListCampaignsInput) (*shared.PaginatedResult[*Campaign], error)
	ListByStatus(status Status) ([]*Campaign, error)
	ListScheduledToStart(at time.Time, limit int) ([]*Campaign, error)
	UpdateStatus(campaignID string, status Status, allowed ...Status) (bool, error)
	UpdateResetCode(campaignID string, resetCode string) error
	UpdateClearCode(campaignID string, clearCode string) error
}
