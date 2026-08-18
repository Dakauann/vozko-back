package instagram

import (
	"time"

	igdomain "vozko/domain/instagram"
)

// AccountResponse is a connected Instagram account. The access token is never
// serialized.
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

	GrantedScopes []string `json:"grantedScopes"`
	// Capability flags are derived server-side so the UI never has to reason
	// about scope strings.
	CanSendMessages   bool `json:"canSendMessages"`
	CanManageComments bool `json:"canManageComments"`
	CanPublish        bool `json:"canPublish"`

	// MessagingHealthy is false when the Instagram-app "Allow Access to
	// Messages" toggle is off, a state in which DMs and webhooks fail silently.
	MessagingHealthy    bool       `json:"messagingHealthy"`
	MessagingCheckedAt  *time.Time `json:"messagingCheckedAt,omitempty"`
	WebhookSubscribedAt *time.Time `json:"webhookSubscribedAt,omitempty"`
	TokenExpiresAt      *time.Time `json:"tokenExpiresAt,omitempty"`
	// NeedsReconnect tells the UI to show a Reconnect call to action.
	NeedsReconnect bool `json:"needsReconnect"`

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

// UpdateAccountConfigRequest edits the automation settings. Every field is a
// pointer so a partial update cannot clear an unrelated setting.
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

// MediaResponse is a post.
//
// MediaURL and ThumbnailURL point at OUR proxy, not at Instagram's CDN: the
// upstream URLs are signed and expire, so handing them to the browser would
// produce broken images.
type MediaResponse struct {
	ID               string     `json:"id"`
	MediaType        string     `json:"mediaType"`
	MediaProductType string     `json:"mediaProductType"`
	IsReel           bool       `json:"isReel"`
	IsCarousel       bool       `json:"isCarousel"`
	Caption          string     `json:"caption,omitempty"`
	Permalink        string     `json:"permalink,omitempty"`
	Shortcode        string     `json:"shortcode,omitempty"`
	Timestamp        *time.Time `json:"timestamp,omitempty"`
	LikeCount        int        `json:"likeCount"`
	CommentsCount    int        `json:"commentsCount"`
	IsCommentEnabled *bool      `json:"isCommentEnabled,omitempty"`
	MediaURL         string     `json:"mediaUrl,omitempty"`
	ThumbnailURL     string     `json:"thumbnailUrl,omitempty"`
	// HasAsset is false when Instagram omitted media_url, which happens for
	// copyrighted content.
	HasAsset bool            `json:"hasAsset"`
	Children []MediaResponse `json:"children,omitempty"`
}

// PageResponse is a cursor-paginated payload.
//
// The cursor is opaque and must not be persisted by the client either: Meta
// documents that cursors can become invalid quickly. HasNext comes from the
// presence of paging.next, a short page does NOT mean the end.
type PageResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasNext    bool   `json:"hasNext"`
}

// CommentResponse is a comment with its replies.
type CommentResponse struct {
	ID           string     `json:"id"`
	Text         string     `json:"text"`
	Timestamp    *time.Time `json:"timestamp,omitempty"`
	FromIGSID    string     `json:"fromIgsid,omitempty"`
	FromUsername string     `json:"fromUsername,omitempty"`
	LikeCount    int        `json:"likeCount"`
	Hidden       bool       `json:"hidden"`
	ParentID     string     `json:"parentId,omitempty"`
	// IsOurs marks a comment we authored. It is also the only case in which
	// deletion is possible, because Instagram requires the comment creator's
	// token to delete.
	IsOurs bool `json:"isOurs"`
	// CanDelete mirrors IsOurs so the UI does not have to know the rule.
	CanDelete bool              `json:"canDelete"`
	Replies   []CommentResponse `json:"replies,omitempty"`
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

// ReplyCommentRequest posts a public reply.
type ReplyCommentRequest struct {
	Message string `json:"message"`
}

// HideCommentRequest hides or unhides a comment.
type HideCommentRequest struct {
	Hidden bool `json:"hidden"`
}

// PrivateReplyRequest DMs a commenter. One per comment, ever, within 7 days.
type PrivateReplyRequest struct {
	Text string `json:"text"`
}

// CreateMediaRequest publishes a post. Captions cannot be edited afterwards.
type CreateMediaRequest struct {
	ImageURL string `json:"imageUrl,omitempty"`
	VideoURL string `json:"videoUrl,omitempty"`
	Caption  string `json:"caption,omitempty"`
	// MediaType is REELS or STORIES; empty publishes a feed image.
	MediaType string `json:"mediaType,omitempty"`
}

// UpdateMediaRequest is the only supported post update.
type UpdateMediaRequest struct {
	CommentEnabled *bool `json:"commentEnabled"`
}

// ConnectStartResponse carries the URL to redirect the browser to.
type ConnectStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}
