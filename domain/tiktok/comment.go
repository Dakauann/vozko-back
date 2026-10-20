package tiktok

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxCommentRunes = 1200

const MaxUsernameRunes = 24

const CommentLookupBatch = 30

const (
	LookupBackoffBase = 30 * time.Second
	MaxLookupBackoff  = time.Hour
	MaxLookupAttempts = 12
)

type CommentAction string

const (
	CommentInserted       CommentAction = "insert"
	CommentDeleted        CommentAction = "delete"
	CommentSetHidden      CommentAction = "set_to_hidden"
	CommentSetFriendsOnly CommentAction = "set_to_friends_only"
	CommentSetPublic      CommentAction = "set_to_public"
)

func ParseCommentAction(raw string) (CommentAction, error) {
	switch a := CommentAction(raw); a {
	case CommentInserted, CommentDeleted, CommentSetHidden, CommentSetFriendsOnly, CommentSetPublic:
		return a, nil
	}
	return "", fmt.Errorf("%w: comment action %q", ErrInvalidContent, raw)
}

type CommentVisibility string

const (
	VisibilityUnknown     CommentVisibility = ""
	VisibilityPublic      CommentVisibility = "public"
	VisibilityHidden      CommentVisibility = "hidden"
	VisibilityFriendsOnly CommentVisibility = "friends_only"
	VisibilityDeleted     CommentVisibility = "deleted"
)

func (a CommentAction) Visibility() CommentVisibility {
	switch a {
	case CommentDeleted:
		return VisibilityDeleted
	case CommentSetHidden:
		return VisibilityHidden
	case CommentSetFriendsOnly:
		return VisibilityFriendsOnly
	case CommentInserted, CommentSetPublic:
		return VisibilityPublic
	}
	return VisibilityUnknown
}

func VisibilityFromStatus(status string) CommentVisibility {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PUBLIC":
		return VisibilityPublic
	case "HIDDEN":
		return VisibilityHidden
	}
	return VisibilityUnknown
}

type CommentKind string

const (
	KindComment CommentKind = "comment"
	KindReply   CommentKind = "reply"
)

type LookupState string

const (
	LookupPending  LookupState = "pending"
	LookupResolved LookupState = "resolved"
	LookupFailed   LookupState = "failed"
)

type Comment struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`

	VideoID         string      `json:"videoId"`
	CommentID       string      `json:"commentId"`
	ParentCommentID string      `json:"parentCommentId,omitempty"`
	Kind            CommentKind `json:"kind"`

	ContactID              *string `json:"contactId,omitempty"`
	AuthorUniqueIdentifier string  `json:"-"`
	AuthorUsername         string  `json:"authorUsername,omitempty"`
	AuthorDisplayName      string  `json:"authorDisplayName,omitempty"`

	Text       string            `json:"text"`
	ImageURL   string            `json:"imageUrl,omitempty"`
	Visibility CommentVisibility `json:"visibility"`
	IsOurs     *bool             `json:"isOurs,omitempty"`
	LikedByUs  bool              `json:"likedByUs"`
	Likes      int               `json:"likes"`
	Replies    int               `json:"replies"`

	CommentedAt *time.Time `json:"commentedAt,omitempty"`
	LastEventAt *time.Time `json:"-"`

	Lookup         LookupState `json:"-"`
	LookupAttempts int         `json:"-"`
	NextLookupAt   *time.Time  `json:"-"`
	RulesDue       bool        `json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Comment) AcceptsEventAt(at time.Time) bool {
	return c.LastEventAt == nil || at.After(*c.LastEventAt)
}

func (c *Comment) ReadyForRules() bool {
	return c.RulesDue && c.IsOurs != nil && !*c.IsOurs && c.Visibility != VisibilityDeleted
}

func (c *Comment) AuthorName() string {
	if u := strings.TrimSpace(c.AuthorUsername); u != "" {
		return u
	}
	return strings.TrimSpace(c.AuthorDisplayName)
}

func LookupBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := LookupBackoffBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= MaxLookupBackoff {
			return MaxLookupBackoff
		}
	}
	return d
}

func ValidateCommentText(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ErrCommentTextEmpty
	}
	if utf8.RuneCountInString(trimmed) > MaxCommentRunes {
		return ErrCommentTextTooLong
	}
	return nil
}
