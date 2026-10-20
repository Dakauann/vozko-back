package tiktok

import "time"

const VideoListPage = 20

type MediaType string

const (
	MediaTypeVideo MediaType = "VIDEO"
	MediaTypePhoto MediaType = "PHOTO"
)

type Video struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	ItemID       string    `json:"itemId"`
	MediaType    MediaType `json:"mediaType"`
	Caption      string    `json:"caption"`
	ShareURL     string    `json:"shareUrl,omitempty"`
	EmbedURL     string    `json:"embedUrl,omitempty"`
	ThumbnailKey string    `json:"-"`
	IsAd         bool      `json:"isAd"`

	Likes    int64 `json:"likes"`
	Comments int64 `json:"comments"`
	Shares   int64 `json:"shares"`
	Views    int64 `json:"views"`

	PostedAt *time.Time `json:"postedAt,omitempty"`
	SyncedAt time.Time  `json:"syncedAt"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
