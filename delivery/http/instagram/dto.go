package instagram

import (
	"time"

	igdomain "vozko/domain/instagram"
)

type AccountResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspaceId"`
	DepartmentID      *string `json:"departmentId,omitempty"`
	IGUserID          string  `json:"igUserId"`
	Username          string  `json:"username"`
	Name              string  `json:"name,omitempty"`
	ProfilePictureURL string  `json:"profilePictureUrl,omitempty"`
	AccountType       string  `json:"accountType,omitempty"`
	FollowersCount    int     `json:"followersCount"`
	FollowsCount      int     `json:"followsCount"`
	MediaCount        int     `json:"mediaCount"`

	Status       string `json:"status"`
	StatusReason string `json:"statusReason,omitempty"`

	GrantedScopes     []string `json:"grantedScopes"`
	CanSendMessages   bool     `json:"canSendMessages"`
	CanManageComments bool     `json:"canManageComments"`
	CanPublish        bool     `json:"canPublish"`

	MessagingHealthy    bool       `json:"messagingHealthy"`
	MessagingCheckedAt  *time.Time `json:"messagingCheckedAt,omitempty"`
	WebhookSubscribedAt *time.Time `json:"webhookSubscribedAt,omitempty"`
	TokenExpiresAt      *time.Time `json:"tokenExpiresAt,omitempty"`
	NeedsReconnect      bool       `json:"needsReconnect"`

	AgentID              *string `json:"agentId,omitempty"`
	WorkflowID           *string `json:"workflowId,omitempty"`
	PipelineID           *string `json:"pipelineId,omitempty"`
	EnableAgentResponses bool    `json:"enableAgentResponses"`
	EnableWorkflow       bool    `json:"enableWorkflow"`
	EnableAnalysis       bool    `json:"enableAnalysis"`
	EnableAutoStaging    bool    `json:"enableAutoStaging"`
	EnableAutoMemory     bool    `json:"enableAutoMemory"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toAccountResponse(a *igdomain.Account) AccountResponse {
	if a == nil {
		return AccountResponse{}
	}
	return AccountResponse{
		ID:                   a.ID,
		WorkspaceID:          a.WorkspaceID,
		DepartmentID:         a.DepartmentID,
		IGUserID:             a.IGUserID,
		Username:             a.Username,
		Name:                 a.Name,
		ProfilePictureURL:    a.ProfilePictureURL,
		AccountType:          a.AccountType,
		FollowersCount:       a.FollowersCount,
		FollowsCount:         a.FollowsCount,
		MediaCount:           a.MediaCount,
		Status:               string(a.Status),
		StatusReason:         a.StatusReason,
		GrantedScopes:        a.GrantedScopes,
		CanSendMessages:      a.CanReceiveMessages(),
		CanManageComments:    a.CanManageComments(),
		CanPublish:           a.CanPublishContent(),
		MessagingHealthy:     a.MessagingHealthy,
		MessagingCheckedAt:   a.MessagingCheckedAt,
		WebhookSubscribedAt:  a.WebhookSubscribedAt,
		TokenExpiresAt:       a.TokenExpiresAt,
		NeedsReconnect:       a.Status == igdomain.StatusTokenExpired || a.Status == igdomain.StatusRevoked,
		AgentID:              a.AgentID,
		WorkflowID:           a.WorkflowID,
		PipelineID:           a.PipelineID,
		EnableAgentResponses: a.EnableAgentResponses,
		EnableWorkflow:       a.EnableWorkflow,
		EnableAnalysis:       a.EnableAnalysis,
		EnableAutoStaging:    a.EnableAutoStaging,
		EnableAutoMemory:     a.EnableAutoMemory,
		CreatedAt:            a.CreatedAt,
		UpdatedAt:            a.UpdatedAt,
	}
}

func toAccountResponses(items []*igdomain.Account) []AccountResponse {
	out := make([]AccountResponse, 0, len(items))
	for _, a := range items {
		out = append(out, toAccountResponse(a))
	}
	return out
}

type UpdateAccountConfigRequest struct {
	DepartmentID         *string `json:"departmentId"`
	AgentID              *string `json:"agentId"`
	WorkflowID           *string `json:"workflowId"`
	PipelineID           *string `json:"pipelineId"`
	EnableAgentResponses *bool   `json:"enableAgentResponses"`
	EnableWorkflow       *bool   `json:"enableWorkflow"`
	EnableAnalysis       *bool   `json:"enableAnalysis"`
	EnableAutoStaging    *bool   `json:"enableAutoStaging"`
	EnableAutoMemory     *bool   `json:"enableAutoMemory"`
}

type MediaResponse struct {
	ID               string          `json:"id"`
	MediaType        string          `json:"mediaType"`
	MediaProductType string          `json:"mediaProductType"`
	IsReel           bool            `json:"isReel"`
	IsCarousel       bool            `json:"isCarousel"`
	Caption          string          `json:"caption,omitempty"`
	Permalink        string          `json:"permalink,omitempty"`
	Shortcode        string          `json:"shortcode,omitempty"`
	Timestamp        *time.Time      `json:"timestamp,omitempty"`
	LikeCount        int             `json:"likeCount"`
	CommentsCount    int             `json:"commentsCount"`
	IsCommentEnabled *bool           `json:"isCommentEnabled,omitempty"`
	MediaURL         string          `json:"mediaUrl,omitempty"`
	ThumbnailURL     string          `json:"thumbnailUrl,omitempty"`
	HasAsset         bool            `json:"hasAsset"`
	Children         []MediaResponse `json:"children,omitempty"`
}

type PageResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasNext    bool   `json:"hasNext"`
}

type CommentResponse struct {
	ID           string            `json:"id"`
	Text         string            `json:"text"`
	Timestamp    *time.Time        `json:"timestamp,omitempty"`
	FromIGSID    string            `json:"fromIgsid,omitempty"`
	FromUsername string            `json:"fromUsername,omitempty"`
	LikeCount    int               `json:"likeCount"`
	Hidden       bool              `json:"hidden"`
	ParentID     string            `json:"parentId,omitempty"`
	IsOurs       bool              `json:"isOurs"`
	CanDelete    bool              `json:"canDelete"`
	Replies      []CommentResponse `json:"replies,omitempty"`
}

func toCommentResponse(c *igdomain.RemoteComment) CommentResponse {
	if c == nil {
		return CommentResponse{}
	}
	out := CommentResponse{
		ID:           c.IGCommentID,
		Text:         c.Text,
		Timestamp:    c.Timestamp,
		FromIGSID:    c.FromIGSID,
		FromUsername: c.FromUsername,
		LikeCount:    c.LikeCount,
		Hidden:       c.Hidden,
		ParentID:     c.ParentID,
		IsOurs:       c.IsOurs,
		CanDelete:    c.IsOurs,
	}
	for _, r := range c.Replies {
		out.Replies = append(out.Replies, toCommentResponse(r))
	}
	return out
}

type ReplyCommentRequest struct {
	Message string `json:"message"`
}

type HideCommentRequest struct {
	Hidden bool `json:"hidden"`
}

type PrivateReplyRequest struct {
	Text string `json:"text"`
}

type CreateMediaRequest struct {
	ImageURL  string `json:"imageUrl,omitempty"`
	VideoURL  string `json:"videoUrl,omitempty"`
	Caption   string `json:"caption,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

type UpdateMediaRequest struct {
	CommentEnabled *bool `json:"commentEnabled"`
}

type ConnectStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}
