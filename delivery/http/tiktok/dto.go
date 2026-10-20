package tiktok

import (
	"time"

	ttdomain "vozko/domain/tiktok"
)

type ConnectStartResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

type CapabilitiesResponse struct {
	CanReadVideos         bool `json:"canReadVideos"`
	CanReadComments       bool `json:"canReadComments"`
	CanManageComments     bool `json:"canManageComments"`
	CanReceiveMessages    bool `json:"canReceiveMessages"`
	CanSendMessages       bool `json:"canSendMessages"`
	CanManageInboxProfile bool `json:"canManageInboxProfile"`
}

type AccountResponse struct {
	ID                   string               `json:"id"`
	WorkspaceID          string               `json:"workspaceId"`
	DepartmentID         *string              `json:"departmentId,omitempty"`
	OpenID               string               `json:"openId"`
	Username             string               `json:"username"`
	DisplayName          string               `json:"displayName"`
	AvatarURL            string               `json:"avatarUrl,omitempty"`
	HasAdvancedAccess    bool                 `json:"hasAdvancedAccess"`
	IsBusinessAccount    bool                 `json:"isBusinessAccount"`
	Status               string               `json:"status"`
	StatusReason         string               `json:"statusReason,omitempty"`
	Capabilities         CapabilitiesResponse `json:"capabilities"`
	GrantedScopes        []string             `json:"grantedScopes"`
	WebhooksHealthy      bool                 `json:"webhooksHealthy"`
	ReconnectBy          *time.Time           `json:"reconnectBy,omitempty"`
	ReconnectSoon        bool                 `json:"reconnectSoon"`
	PurgeAfter           *time.Time           `json:"purgeAfter,omitempty"`
	LastHealthCheckAt    *time.Time           `json:"lastHealthCheckAt,omitempty"`
	AgentID              *string              `json:"agentId,omitempty"`
	WorkflowID           *string              `json:"workflowId,omitempty"`
	PipelineID           *string              `json:"pipelineId,omitempty"`
	EnableAgentResponses bool                 `json:"enableAgentResponses"`
	EnableWorkflow       bool                 `json:"enableWorkflow"`
	EnableAnalysis       bool                 `json:"enableAnalysis"`
	EnableAutoStaging    bool                 `json:"enableAutoStaging"`
	EnableAutoMemory     bool                 `json:"enableAutoMemory"`
	AutomationDisclosure string               `json:"automationDisclosure"`
	CreatedAt            time.Time            `json:"createdAt"`
	UpdatedAt            time.Time            `json:"updatedAt"`
}

type UpdateAccountRequest struct {
	DepartmentID         *string `json:"departmentId"`
	AgentID              *string `json:"agentId"`
	WorkflowID           *string `json:"workflowId"`
	PipelineID           *string `json:"pipelineId"`
	EnableAgentResponses *bool   `json:"enableAgentResponses"`
	EnableWorkflow       *bool   `json:"enableWorkflow"`
	EnableAnalysis       *bool   `json:"enableAnalysis"`
	EnableAutoStaging    *bool   `json:"enableAutoStaging"`
	EnableAutoMemory     *bool   `json:"enableAutoMemory"`
	AutomationDisclosure *string `json:"automationDisclosure"`
}

type DisconnectResponse struct {
	PurgeAfter time.Time `json:"purgeAfter"`
	Warning    string    `json:"warning,omitempty"`
}

type VideoResponse struct {
	ItemID       string     `json:"itemId"`
	MediaType    string     `json:"mediaType"`
	Caption      string     `json:"caption"`
	ShareURL     string     `json:"shareUrl,omitempty"`
	EmbedURL     string     `json:"embedUrl,omitempty"`
	ThumbnailURL string     `json:"thumbnailUrl,omitempty"`
	IsAd         bool       `json:"isAd"`
	Likes        int64      `json:"likes"`
	Comments     int64      `json:"comments"`
	Shares       int64      `json:"shares"`
	Views        int64      `json:"views"`
	PostedAt     *time.Time `json:"postedAt,omitempty"`
}

type VideoPageResponse struct {
	Videos     []VideoResponse `json:"videos"`
	NextCursor string          `json:"nextCursor,omitempty"`
	HasMore    bool            `json:"hasMore"`
}

type CommentResponse struct {
	CommentID         string     `json:"commentId"`
	VideoID           string     `json:"videoId"`
	ParentCommentID   string     `json:"parentCommentId,omitempty"`
	Kind              string     `json:"kind"`
	AuthorUsername    string     `json:"authorUsername,omitempty"`
	AuthorDisplayName string     `json:"authorDisplayName,omitempty"`
	ContactID         *string    `json:"contactId,omitempty"`
	Text              string     `json:"text"`
	ImageURL          string     `json:"imageUrl,omitempty"`
	Visibility        string     `json:"visibility"`
	IsOurs            bool       `json:"isOurs"`
	LikedByUs         bool       `json:"likedByUs"`
	Likes             int        `json:"likes"`
	Replies           int        `json:"replies"`
	CommentedAt       *time.Time `json:"commentedAt,omitempty"`
	CanReply          bool       `json:"canReply"`
	CanHide           bool       `json:"canHide"`
	CanLike           bool       `json:"canLike"`
}

type CommentPageResponse struct {
	Comments   []CommentResponse `json:"comments"`
	NextCursor string            `json:"nextCursor,omitempty"`
	HasMore    bool              `json:"hasMore"`
}

type ReplyRequest struct {
	Text string `json:"text"`
}

func presentComment(c *ttdomain.Comment, canManage bool) CommentResponse {
	ours := c.IsOurs != nil && *c.IsOurs
	live := c.Visibility != ttdomain.VisibilityDeleted
	return CommentResponse{
		CommentID: c.CommentID, VideoID: c.VideoID, ParentCommentID: c.ParentCommentID, Kind: string(c.Kind),
		AuthorUsername: c.AuthorUsername, AuthorDisplayName: c.AuthorDisplayName, ContactID: c.ContactID,
		Text: c.Text, ImageURL: c.ImageURL, Visibility: string(c.Visibility), IsOurs: ours, LikedByUs: c.LikedByUs,
		Likes: c.Likes, Replies: c.Replies, CommentedAt: c.CommentedAt,
		CanReply: canManage && live, CanHide: canManage && live && !ours, CanLike: canManage && live,
	}
}
