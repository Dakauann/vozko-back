package unofficial_whatsapp

import (
	"time"

	uw "vozko/domain/unofficial_whatsapp"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type instanceDTO struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspaceId"`
	DepartmentID *string `json:"departmentId,omitempty"`
	Provider     string  `json:"provider"`

	DisplayName    string `json:"displayName"`
	PhoneNumber    string `json:"phoneNumber,omitempty"`
	ProfileName    string `json:"profileName,omitempty"`
	ProfilePicURL  string `json:"profilePicUrl,omitempty"`
	IsBusinessAcct bool   `json:"isBusinessAccount"`
	Platform       string `json:"platform,omitempty"`

	Status       string `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`
	SessionLive  bool   `json:"sessionLive"`

	ConnectedAt          *time.Time `json:"connectedAt,omitempty"`
	LastDisconnectAt     *time.Time `json:"lastDisconnectAt,omitempty"`
	LastDisconnectReason string     `json:"lastDisconnectReason,omitempty"`
	LastPolledAt         *time.Time `json:"lastPolledAt,omitempty"`
	WebhookSetAt         *time.Time `json:"webhookSetAt,omitempty"`

	Restriction restrictionDTO `json:"restriction"`

	DailySendCap    int        `json:"dailySendCap"`
	SendDelayMinMS  int        `json:"sendDelayMinMs"`
	SendDelayMaxMS  int        `json:"sendDelayMaxMs"`
	AutoRejectCalls bool       `json:"autoRejectCalls"`
	WarmupStartedAt *time.Time `json:"warmupStartedAt,omitempty"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`
	HandleGroups         bool    `json:"handleGroups"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type restrictionDTO struct {
	Active     bool       `json:"active"`
	Key        string     `json:"key,omitempty"`
	Message    string     `json:"message,omitempty"`
	Until      *time.Time `json:"until,omitempty"`
	UsedQuota  int        `json:"usedQuota,omitempty"`
	TotalQuota int        `json:"totalQuota,omitempty"`
	CheckedAt  *time.Time `json:"checkedAt,omitempty"`
}

func toInstanceDTO(i *uw.Instance) instanceDTO {
	if i == nil {
		return instanceDTO{}
	}
	now := time.Now().UTC()
	return instanceDTO{
		ID:           i.ID,
		WorkspaceID:  i.WorkspaceID,
		DepartmentID: i.DepartmentID,
		Provider:     i.Provider,

		DisplayName:    i.Label(),
		PhoneNumber:    i.PhoneNumber,
		ProfileName:    i.ProfileName,
		ProfilePicURL:  i.ProfilePicURL,
		IsBusinessAcct: i.IsBusinessAcct,
		Platform:       i.Platform,

		Status:       string(i.Status),
		StatusReason: i.StatusReason,
		SessionLive:  i.SessionLive(),

		ConnectedAt:          i.ConnectedAt,
		LastDisconnectAt:     i.LastDisconnectAt,
		LastDisconnectReason: i.LastDisconnectWhy,
		LastPolledAt:         i.LastPolledAt,
		WebhookSetAt:         i.WebhookSetAt,

		Restriction: restrictionDTO{
			Active:     i.Restriction.Active(now),
			Key:        i.Restriction.Key,
			Message:    i.Restriction.Message,
			Until:      i.Restriction.Until,
			UsedQuota:  i.Restriction.UsedQuota,
			TotalQuota: i.Restriction.TotalQuota,
			CheckedAt:  i.Restriction.CheckedAt,
		},

		DailySendCap:    i.DailySendCap,
		SendDelayMinMS:  i.SendDelayMinMS,
		SendDelayMaxMS:  i.SendDelayMaxMS,
		AutoRejectCalls: i.AutoRejectCalls,
		WarmupStartedAt: i.WarmupStartedAt,

		AgentID:              i.AgentID,
		WorkflowID:           i.WorkflowID,
		PipelineID:           i.PipelineID,
		EnableAgentResponses: i.EnableAgentResponses,
		EnableWorkflow:       i.EnableWorkflow,
		EnableAnalysis:       i.EnableAnalysis,
		EnableAutoStaging:    i.EnableAutoStaging,
		EnableAutoMemory:     i.EnableAutoMemory,
		HandleGroups:         i.HandleGroups,

		CreatedAt: i.CreatedAt,
		UpdatedAt: i.UpdatedAt,
	}
}

type linkChallengeDTO struct {
	Instance  instanceDTO `json:"instance"`
	QRCode    string      `json:"qrCode,omitempty"`
	PairCode  string      `json:"pairCode,omitempty"`
	ExpiresAt *time.Time  `json:"expiresAt,omitempty"`
}

func toLinkChallengeDTO(c *uwuc.LinkChallenge) linkChallengeDTO {
	if c == nil {
		return linkChallengeDTO{}
	}
	return linkChallengeDTO{
		Instance:  toInstanceDTO(c.Instance),
		QRCode:    c.QRCode,
		PairCode:  c.PairCode,
		ExpiresAt: c.ExpiresAt,
	}
}
