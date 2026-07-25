package whatsapp_campaign

import "vozko/domain/shared"

type ListCampaignsInput struct {
	WorkspaceID    string
	DepartmentIDs  []string
	Search         string
	Type           CampaignType
	TemplateIDs    []string
	Archived       *bool
	Options        shared.QueryOptions
	ScheduledStart shared.Filter
}
