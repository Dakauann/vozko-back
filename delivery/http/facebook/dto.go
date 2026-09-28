package facebook

import (
	"time"

	fbdomain "vozko/domain/facebook"
)

type ConnectStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

type CapabilitiesResponse struct {
	Messaging bool `json:"messaging"`
	ReadPosts bool `json:"readPosts"`
	Publish   bool `json:"publish"`
	Moderate  bool `json:"moderate"`
	Comment   bool `json:"comment"`
	Subscribe bool `json:"subscribe"`
}

type RoutingResponse struct {
	IsDefaultApp *bool      `json:"isDefaultApp"`
	CheckedAt    *time.Time `json:"checkedAt,omitempty"`
}

type PolicyResponse struct {
	Action string     `json:"action,omitempty"`
	Reason string     `json:"reason,omitempty"`
	At     *time.Time `json:"at,omitempty"`
}

type PageResponse struct {
	ID                    string  `json:"id"`
	WorkspaceID           string  `json:"workspaceId"`
	DepartmentID          *string `json:"departmentId,omitempty"`
	FBPageID              string  `json:"fbPageId"`
	Name                  string  `json:"name"`
	Username              string  `json:"username,omitempty"`
	Category              string  `json:"category,omitempty"`
	Link                  string  `json:"link,omitempty"`
	PictureURL            string  `json:"pictureUrl,omitempty"`
	FollowersCount        int     `json:"followersCount"`
	LinkedInstagramUserID string  `json:"linkedInstagramUserId,omitempty"`

	Status         string               `json:"status"`
	StatusReason   string               `json:"statusReason,omitempty"`
	Tasks          []string             `json:"tasks"`
	GrantedScopes  []string             `json:"grantedScopes"`
	Capabilities   CapabilitiesResponse `json:"capabilities"`
	NeedsReconnect bool                 `json:"needsReconnect"`

	WebhookSubscribedAt *time.Time      `json:"webhookSubscribedAt,omitempty"`
	SubscribedFields    []string        `json:"subscribedFields"`
	Routing             RoutingResponse `json:"routing"`
	Policy              PolicyResponse  `json:"policy"`
	HumanAgentAvailable bool            `json:"humanAgentAvailable"`
	HealthCheckedAt     *time.Time      `json:"healthCheckedAt,omitempty"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
	AutomationDisclosure string  `json:"automationDisclosure"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type UpdatePageRequest struct {
	DepartmentID         *string `json:"departmentId"`
	AgentID              *string `json:"agentId"`
	WorkflowID           *string `json:"workflowId"`
	PipelineID           *string `json:"pipelineId"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
	AutomationDisclosure string  `json:"automationDisclosure"`
}

type pagePresenter struct {
	pictureURL          func(key string) string
	humanAgentAvailable bool
}

func (p pagePresenter) page(page *fbdomain.Page) PageResponse {
	tasks := make([]string, 0, len(page.Tasks))
	for _, t := range page.Tasks {
		tasks = append(tasks, string(t))
	}
	return PageResponse{
		ID:                    page.ID,
		WorkspaceID:           page.WorkspaceID,
		DepartmentID:          page.DepartmentID,
		FBPageID:              page.FBPageID,
		Name:                  page.Name,
		Username:              page.Username,
		Category:              page.Category,
		Link:                  page.Link,
		PictureURL:            p.pictureURL(page.PictureStorageKey),
		FollowersCount:        page.FollowersCount,
		LinkedInstagramUserID: page.LinkedIGUserID,
		Status:                string(page.Status),
		StatusReason:          page.StatusReason,
		Tasks:                 tasks,
		GrantedScopes:         nonNil(page.GrantedScopes),
		Capabilities: CapabilitiesResponse{
			Messaging: page.Can(fbdomain.CapMessaging),
			ReadPosts: page.Can(fbdomain.CapReadPosts),
			Publish:   page.Can(fbdomain.CapPublish),
			Moderate:  page.Can(fbdomain.CapModerate),
			Comment:   page.Can(fbdomain.CapComment),
			Subscribe: page.Can(fbdomain.CapSubscribe),
		},
		NeedsReconnect:       page.NeedsReconnect(),
		WebhookSubscribedAt:  page.WebhookSubscribedAt,
		SubscribedFields:     nonNil(page.SubscribedFields),
		Routing:              RoutingResponse{IsDefaultApp: page.IsDefaultRouteApp, CheckedAt: page.RoutingCheckedAt},
		Policy:               PolicyResponse{Action: page.PolicyAction, Reason: page.PolicyReason, At: page.PolicyAt},
		HumanAgentAvailable:  p.humanAgentAvailable,
		HealthCheckedAt:      page.HealthCheckedAt,
		AgentID:              page.AgentID,
		WorkflowID:           page.WorkflowID,
		PipelineID:           page.PipelineID,
		EnableAgentResponses: page.EnableAgentResponses,
		EnableWorkflow:       page.EnableWorkflow,
		EnableAnalysis:       page.EnableAnalysis,
		EnableAutoStaging:    page.EnableAutoStaging,
		EnableAutoMemory:     page.EnableAutoMemory,
		AutomationDisclosure: page.AutomationDisclosure,
		CreatedAt:            page.CreatedAt,
		UpdatedAt:            page.UpdatedAt,
	}
}

func (p pagePresenter) pages(pages []*fbdomain.Page) []PageResponse {
	out := make([]PageResponse, 0, len(pages))
	for _, page := range pages {
		out = append(out, p.page(page))
	}
	return out
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}
