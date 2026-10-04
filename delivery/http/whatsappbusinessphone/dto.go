package whatsappbusinessphone

import wc "vozko/domain/whatsapp_campaign"

type RequestVerificationRequest struct {
	Method   string `json:"method" example:"SMS"`
	Language string `json:"language" example:"pt_BR"`
}

type VerifyCodeRequest struct {
	Code string `json:"code" example:"123456"`
}

type UpdateBusinessProfileRequest struct {
	About                string   `json:"about"`
	Address              string   `json:"address"`
	Description          string   `json:"description"`
	Email                string   `json:"email"`
	ProfilePictureURL    string   `json:"profilePictureUrl"`
	ProfilePictureHandle string   `json:"profilePictureHandle"`
	Websites             []string `json:"websites"`
	Vertical             string   `json:"vertical"`
}

type RegisterPhoneRequest struct {
	Pin string `json:"pin" example:"123456"`
}

type ReleasePhoneRequest struct {
	ConfirmPhoneNumber string `json:"confirmPhoneNumber" example:"5511987654321"`
}

type AssignOwnerRequest struct {
	WorkspaceID string `json:"workspaceId" example:"ws_a1b2c3"`
}

type SetCallingStatusRequest struct {
	Enabled bool `json:"enabled" example:"true"`
}

type NumberAutomationRequest struct {
	AgentID              *string `json:"agentId" example:"6f1c2d3e-4a5b-4c6d-8e9f-0a1b2c3d4e5f"`
	WorkflowID           *string `json:"workflowId"`
	PipelineID           *string `json:"pipelineId"`
	EnableAgentResponses bool    `json:"enableAgentResponses" example:"true"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
}

func (r NumberAutomationRequest) settings() wc.ReceptiveSettings {
	return wc.ReceptiveSettings{
		AgentID:              valueOf(r.AgentID),
		WorkflowID:           valueOf(r.WorkflowID),
		PipelineID:           valueOf(r.PipelineID),
		EnableAgentResponses: r.EnableAgentResponses,
		EnableWorkflow:       r.EnableWorkflow,
		EnableAnalysis:       r.EnableAnalysis,
		EnableAutoStaging:    r.EnableAutoStaging,
		EnableAutoMemory:     r.EnableAutoMemory,
	}
}

type NumberAutomationResponse struct {
	ID                   string  `json:"id" example:"7a8b9c0d-1e2f-4a3b-9c4d-5e6f7a8b9c0d"`
	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
}

func numberAutomationResponse(phoneID string, s wc.ReceptiveSettings) NumberAutomationResponse {
	return NumberAutomationResponse{
		ID:                   phoneID,
		AgentID:              optional(s.AgentID),
		WorkflowID:           optional(s.WorkflowID),
		PipelineID:           optional(s.PipelineID),
		EnableAgentResponses: s.EnableAgentResponses,
		EnableWorkflow:       s.EnableWorkflow,
		EnableAnalysis:       s.EnableAnalysis,
		EnableAutoStaging:    s.EnableAutoStaging,
		EnableAutoMemory:     s.EnableAutoMemory,
	}
}

func valueOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type MessageResponse struct {
	Message string `json:"message" example:"WhatsApp Business phone number synchronized successfully"`
}

type CallingStatusResponse struct {
	Enabled bool `json:"enabled" example:"true"`
}

type VerticalOption struct {
	Value string `json:"value" example:"FINANCE"`
	Label string `json:"label" example:"Finance and Banking"`
}
