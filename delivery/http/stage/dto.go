package stage

type CreateTagRequest struct {
	Name         string `json:"name" example:"Em atendimento"`
	Description  string `json:"description,omitempty" example:"Conversas em andamento"`
	Color        string `json:"color,omitempty" example:"#2463eb"`
	CampaignID   string `json:"campaignId"`
	CampaignType string `json:"campaignType"`
}

type UpdateTagRequest struct {
	Name        *string `json:"name,omitempty" example:"Em atendimento"`
	Description *string `json:"description,omitempty" example:"Conversas em andamento"`
	Color       *string `json:"color,omitempty" example:"#2463eb"`
}

type AssignEntryTagRequest struct {
	StageID   string `json:"StageID" example:"stg_a1b2c3"`
	EntryID   string `json:"entryId" example:"call_a1b2c3"`
	EntryType string `json:"entryType" example:"whatsapp"`
}

type RemoveEntryTagRequest struct {
	StageID   string `json:"StageID" example:"stg_a1b2c3"`
	EntryID   string `json:"entryId" example:"call_a1b2c3"`
	EntryType string `json:"entryType" example:"whatsapp"`
}

type SetInitialTagRequest struct {
	StageID string `json:"StageID" example:"stg_a1b2c3"`
}

type ReorderTagsRequest struct {
	StageIDs     []string `json:"stageIds" example:"stg_a1b2c3,stg_d4e5f6"`
	CampaignID   string   `json:"campaignId"`
	CampaignType string   `json:"campaignType"`
}

type MessageResponse struct {
	Message string `json:"message" example:"tag removed"`
}
