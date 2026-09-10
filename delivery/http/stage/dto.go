package stage

type CreateTagRequest struct {
	Name         string `json:"name" example:"Em atendimento"`
	Description  string `json:"description,omitempty" example:"Conversas em andamento"`
	Color        string `json:"color,omitempty" example:"#2463eb"`
	CampaignID   string `json:"campaignId"`
	CampaignType string `json:"campaignType"`
	// PipelineID is the funnel the new stage joins. Omit it and the stage lands on
	// the workspace default funnel.
	PipelineID string `json:"pipelineId,omitempty" example:"pl_a1b2c3"`
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

	// MoveToFunnel authorises landing on a stage of a DIFFERENT funnel.
	//
	// Without it a cross-funnel move is refused, which is what stops a stage
	// list showing the wrong funnel from stranding a lead on a board nobody
	// looks at. The UI sets it only after the operator has deliberately picked a
	// target funnel, so an ordinary move between columns keeps the guard.
	MoveToFunnel bool `json:"moveToFunnel,omitempty"`
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
	PipelineID   string   `json:"pipelineId,omitempty" example:"pl_a1b2c3"`
}

type MessageResponse struct {
	Message string `json:"message" example:"tag removed"`
}
