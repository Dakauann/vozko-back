package facebook

import (
	"errors"
	"time"
)

var (
	ErrCommentNotFound = errors.New("facebook comment not found")
	ErrCommentNotOurs  = errors.New("only comments the page wrote can be edited")
	ErrCommentEmpty    = errors.New("comment text is required")
)

type CommentFilter string

const (
	CommentsStream   CommentFilter = "stream"
	CommentsTopLevel CommentFilter = "toplevel"
)

type RemoteComment struct {
	FBCommentID       string
	ParentID          string
	FromID            string
	FromName          string
	Message           string
	CreatedTime       *time.Time
	LikeCount         int
	CommentCount      int
	IsHidden          bool
	UserLikes         bool
	CanHide           bool
	CanRemove         bool
	CanReplyPrivately bool
	CanLike           bool
	AttachmentType    string
	AttachmentURL     string
}

type Comment struct {
	ID                string
	WorkspaceID       string
	PageID            string
	FBCommentID       string
	FBPostID          string
	ParentFBCommentID *string
	FromID            *string
	FromName          string
	FromIsPage        bool
	ContactID         *string
	Message           string
	AttachmentType    string
	LikeCount         int
	ReplyCount        int
	IsHidden          bool
	IsOurs            bool
	LikedByPage       bool
	CreatedTime       *time.Time
	EditedAt          *time.Time
	RemovedAt         *time.Time
}

func (c *Comment) TopLevel() bool {
	return c.ParentFBCommentID == nil || *c.ParentFBCommentID == "" || *c.ParentFBCommentID == c.FBPostID
}

func CommentFromRemote(page *Page, fbPostID string, remote *RemoteComment) *Comment {
	out := &Comment{
		WorkspaceID:    page.WorkspaceID,
		PageID:         page.ID,
		FBCommentID:    remote.FBCommentID,
		FBPostID:       fbPostID,
		FromName:       remote.FromName,
		Message:        remote.Message,
		AttachmentType: remote.AttachmentType,
		LikeCount:      remote.LikeCount,
		ReplyCount:     remote.CommentCount,
		IsHidden:       remote.IsHidden,
		LikedByPage:    remote.UserLikes,
		CreatedTime:    remote.CreatedTime,
	}
	if remote.ParentID != "" {
		parent := remote.ParentID
		out.ParentFBCommentID = &parent
	}
	if remote.FromID != "" {
		from := remote.FromID
		out.FromID = &from
		out.FromIsPage = from == page.FBPageID
		out.IsOurs = out.FromIsPage
	}
	return out
}

func (p *Page) CommentFromFeed(ev *FeedEvent) *Comment {
	out := &Comment{
		WorkspaceID:    p.WorkspaceID,
		PageID:         p.ID,
		FBCommentID:    ev.CommentID,
		FBPostID:       ev.PostID,
		Message:        ev.Message,
		AttachmentType: attachmentTypeOf(ev),
		CreatedTime:    timePtr(ev.CreatedTime),
	}
	if ev.ParentID != "" {
		parent := ev.ParentID
		out.ParentFBCommentID = &parent
	}
	if ev.From != nil {
		from := ev.From.ID
		out.FromID, out.FromName = &from, ev.From.Name
		out.FromIsPage = from == p.FBPageID
		out.IsOurs = out.FromIsPage
	}
	return out
}

func attachmentTypeOf(ev *FeedEvent) string {
	if ev.Photo != "" {
		return "photo"
	}
	return ""
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

type ReactionDelta int

func ReactionDeltaFor(verb string) ReactionDelta {
	switch verb {
	case "add":
		return 1
	case "remove":
		return -1
	}
	return 0
}
