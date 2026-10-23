package mercadolivre

import "time"

type AccountResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`

	MLUserID    string `json:"mlUserId"`
	SiteID      string `json:"siteId"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"displayName"`

	Status         string `json:"status"`
	StatusReason   string `json:"statusReason,omitempty"`
	NeedsReconnect bool   `json:"needsReconnect"`

	GrantedScopes      []string `json:"grantedScopes"`
	CanReadQuestions   bool     `json:"canReadQuestions"`
	CanAnswerQuestions bool     `json:"canAnswerQuestions"`
	CanReadMessages    bool     `json:"canReadMessages"`
	CanSendMessages    bool     `json:"canSendMessages"`

	AccessExpiresAt   *time.Time `json:"accessExpiresAt,omitempty"`
	LastReconciledAt  *time.Time `json:"lastReconciledAt,omitempty"`
	LastHealthCheckAt *time.Time `json:"lastHealthCheckAt,omitempty"`
	PurgeAfter        *time.Time `json:"purgeAfter,omitempty"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	AutomationDisclosure string  `json:"automationDisclosure"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type AccountListResponse struct {
	Data []AccountResponse `json:"data"`
	Meta PaginationMeta    `json:"meta"`
}

type PaginationMeta struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"pageSize"`
	TotalPages int   `json:"totalPages"`
	TotalItems int64 `json:"totalItems"`
}

type ConnectStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

type UpdateAccountRequest struct {
	DepartmentID         *string `json:"departmentId"`
	AgentID              *string `json:"agentId"`
	WorkflowID           *string `json:"workflowId"`
	PipelineID           *string `json:"pipelineId"`
	EnableAgentResponses *bool   `json:"enableAgentResponses"`
	EnableWorkflow       *bool   `json:"enableWorkflow"`
	AutomationDisclosure *string `json:"automationDisclosure"`
}

type DisconnectResponse struct {
	Status     string    `json:"status"`
	PurgeAfter time.Time `json:"purgeAfter"`
	Warning    string    `json:"warning,omitempty"`
}

type NotificationAck struct {
	Status string `json:"status"`
}
