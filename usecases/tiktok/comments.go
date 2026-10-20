package tiktok

import (
	"context"
	"errors"
	"log"
	"strings"

	ttdomain "vozko/domain/tiktok"
	"vozko/pkg/bounded"
)

const thumbnailRehostConcurrency = 4

type CommentsDeps struct {
	Accounts ttdomain.AccountRepository
	Videos   ttdomain.VideoRepository
	Comments ttdomain.CommentRepository
	Tokens   *TokenSource
	Services Services
	Images   *ImageStore
	Metrics  Metrics
}

type Comments struct {
	d   CommentsDeps
	now Clock
}

func NewComments(d CommentsDeps) *Comments {
	if d.Accounts == nil || d.Videos == nil || d.Comments == nil || d.Tokens == nil || d.Images == nil || d.Metrics == nil ||
		d.Services.Videos == nil || d.Services.Comments == nil || d.Services.Errors == nil {
		panic("tiktok: comments require accounts, videos, comments, tokens, the video and comment services, an error classifier, images and metrics")
	}
	return &Comments{d: d, now: utcNow}
}

type VideoListing struct {
	Videos  []*ttdomain.Video
	Cursor  int64
	HasMore bool
}

func (uc *Comments) ListVideos(ctx context.Context, workspaceID, accountID string, cursor int64) (*VideoListing, error) {
	account, err := uc.readable(ctx, workspaceID, accountID, func(a *ttdomain.Account) bool { return a.CanReadVideosNow() })
	if err != nil {
		return nil, err
	}
	creds, err := uc.d.Tokens.Credentials(ctx, account)
	if err != nil {
		return nil, err
	}
	page, err := uc.d.Services.Videos.ListVideos(ctx, creds, cursor, ttdomain.VideoListPage)
	if err != nil {
		return nil, err
	}
	videos := uc.mirrorVideos(ctx, account, page.Videos)
	return &VideoListing{Videos: videos, Cursor: page.Cursor, HasMore: page.HasMore}, nil
}

func (uc *Comments) mirrorVideos(ctx context.Context, account *ttdomain.Account, remote []ttdomain.RemoteVideo) []*ttdomain.Video {
	ids := make([]string, 0, len(remote))
	for _, rv := range remote {
		ids = append(ids, rv.ItemID)
	}
	existing, err := uc.d.Videos.FindByItemIDs(ctx, account.ID, ids)
	if err != nil {
		log.Printf("[tiktok] video mirror of %s unreadable: %v", account.ID, err)
	}
	known := make(map[string]string, len(existing))
	for _, v := range existing {
		known[v.ItemID] = v.ThumbnailKey
	}
	now := uc.now()
	videos := make([]*ttdomain.Video, len(remote))
	for i, rv := range remote {
		videos[i] = &ttdomain.Video{
			WorkspaceID: account.WorkspaceID, AccountID: account.ID, ItemID: rv.ItemID, MediaType: rv.MediaType, Caption: rv.Caption,
			ShareURL: rv.ShareURL, EmbedURL: rv.EmbedURL, ThumbnailKey: known[rv.ItemID], IsAd: rv.IsAd,
			Likes: rv.Likes, Comments: rv.Comments, Shares: rv.Shares, Views: rv.Views, PostedAt: rv.PostedAt, SyncedAt: now,
		}
	}
	type job struct {
		video  *ttdomain.Video
		source string
	}
	var jobs []job
	for i, v := range videos {
		if v.ThumbnailKey == "" && remote[i].ThumbnailURL != "" {
			jobs = append(jobs, job{video: v, source: remote[i].ThumbnailURL})
		}
	}
	bounded.ForEach(ctx, jobs, thumbnailRehostConcurrency, func(ctx context.Context, j job) {
		key, err := uc.d.Images.Rehost(ctx, VideoThumbnailKey(account.ID, j.video.ItemID), j.source)
		if err != nil {
			uc.d.Metrics.AddTikTokEvent("video", "thumbnail_failed")
			return
		}
		j.video.ThumbnailKey = key
	})
	if err := uc.d.Videos.UpsertMany(ctx, videos); err != nil {
		log.Printf("[tiktok] video mirror of %s not saved: %v", account.ID, err)
	}
	return videos
}

func (uc *Comments) readable(ctx context.Context, workspaceID, accountID string, can func(*ttdomain.Account) bool) (*ttdomain.Account, error) {
	account, err := uc.d.Accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	if !can(account) {
		return nil, ErrAccountUnavailable
	}
	return account, nil
}

type CommentListing struct {
	Comments []*ttdomain.Comment
	Cursor   int64
	HasMore  bool
}

func (uc *Comments) ListComments(ctx context.Context, workspaceID, accountID, videoID string, cursor int64, includeHidden bool) (*CommentListing, error) {
	if !ttdomain.ValidNumericID(videoID) {
		return nil, ttdomain.ErrVideoNotFound
	}
	account, err := uc.readable(ctx, workspaceID, accountID, func(a *ttdomain.Account) bool { return a.CanReadCommentsNow() })
	if err != nil {
		return nil, err
	}
	creds, err := uc.d.Tokens.Credentials(ctx, account)
	if err != nil {
		return nil, err
	}
	page, err := uc.d.Services.Comments.ListComments(ctx, creds, ttdomain.CommentQuery{VideoID: videoID, Cursor: cursor, IncludeHidden: includeHidden, IncludeReplies: true})
	if err != nil {
		return nil, err
	}
	comments := make([]*ttdomain.Comment, 0, len(page.Comments))
	for _, rc := range page.Comments {
		comments = append(comments, mirroredComment(account, rc))
	}
	if err := uc.d.Comments.MirrorMany(ctx, comments); err != nil {
		log.Printf("[tiktok] comment mirror of %s not saved: %v", account.ID, err)
	}
	return &CommentListing{Comments: comments, Cursor: page.Cursor, HasMore: page.HasMore}, nil
}

func mirroredComment(account *ttdomain.Account, rc ttdomain.RemoteComment) *ttdomain.Comment {
	ours := rc.Owner
	kind := ttdomain.KindComment
	if rc.ParentCommentID != "" {
		kind = ttdomain.KindReply
	}
	return &ttdomain.Comment{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, VideoID: rc.VideoID, CommentID: rc.CommentID, ParentCommentID: rc.ParentCommentID, Kind: kind,
		AuthorUniqueIdentifier: rc.UniqueIdentifier, AuthorUsername: rc.Username, AuthorDisplayName: rc.DisplayName,
		Text: rc.Text, ImageURL: rc.ImageURL, Visibility: rc.Visibility, IsOurs: &ours, LikedByUs: rc.Liked, Likes: rc.Likes, Replies: rc.Replies,
		CommentedAt: rc.CreatedAt, Lookup: ttdomain.LookupResolved,
	}
}

func (uc *Comments) Reply(ctx context.Context, workspaceID, accountID, commentID, text string) (*ttdomain.Comment, error) {
	account, err := uc.readable(ctx, workspaceID, accountID, func(a *ttdomain.Account) bool { return a.CanManageCommentsNow() })
	if err != nil {
		return nil, err
	}
	return uc.reply(ctx, account, commentID, text)
}

func (uc *Comments) reply(ctx context.Context, account *ttdomain.Account, commentID, text string) (*ttdomain.Comment, error) {
	text = strings.TrimSpace(text)
	if err := ttdomain.ValidateCommentText(text); err != nil {
		return nil, err
	}
	target, creds, err := uc.owned(ctx, account, commentID)
	if err != nil {
		return nil, err
	}
	parent := target.CommentID
	if target.ParentCommentID != "" {
		parent = target.ParentCommentID
	}
	newID, err := uc.d.Services.Comments.Reply(ctx, creds, target.VideoID, parent, text)
	if err != nil {
		uc.d.Metrics.AddTikTokEvent("comment", "reply_failed")
		return nil, err
	}
	now := uc.now()
	ours := true
	reply := &ttdomain.Comment{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, VideoID: target.VideoID, CommentID: newID, ParentCommentID: parent,
		Kind: ttdomain.KindReply, AuthorUsername: account.Username, AuthorDisplayName: account.DisplayName, Text: text,
		Visibility: ttdomain.VisibilityPublic, IsOurs: &ours, CommentedAt: &now, Lookup: ttdomain.LookupResolved,
	}
	if err := uc.d.Comments.RecordOurs(ctx, reply); err != nil {
		log.Printf("[tiktok] reply %s not mirrored as ours, its webhook will be checked by author: %v", newID, err)
	}
	uc.d.Metrics.AddTikTokEvent("comment", "replied")
	return reply, nil
}

func (uc *Comments) owned(ctx context.Context, account *ttdomain.Account, commentID string) (*ttdomain.Comment, ttdomain.Credentials, error) {
	if !ttdomain.ValidNumericID(commentID) {
		return nil, ttdomain.Credentials{}, ttdomain.ErrCommentNotFound
	}
	target, err := uc.d.Comments.FindByCommentID(ctx, account.ID, commentID)
	if errors.Is(err, ttdomain.ErrCommentNotFound) {
		return nil, ttdomain.Credentials{}, ttdomain.ErrCommentNotOwned
	}
	if err != nil {
		return nil, ttdomain.Credentials{}, err
	}
	if target.Visibility == ttdomain.VisibilityDeleted {
		return nil, ttdomain.Credentials{}, ttdomain.ErrCommentNotFound
	}
	creds, err := uc.d.Tokens.Credentials(ctx, account)
	if err != nil {
		return nil, ttdomain.Credentials{}, err
	}
	return target, creds, nil
}

func (uc *Comments) SetHidden(ctx context.Context, workspaceID, accountID, commentID string, hidden bool) (*ttdomain.Comment, error) {
	account, err := uc.readable(ctx, workspaceID, accountID, func(a *ttdomain.Account) bool { return a.CanManageCommentsNow() })
	if err != nil {
		return nil, err
	}
	return uc.setHidden(ctx, account, commentID, hidden)
}

func (uc *Comments) setHidden(ctx context.Context, account *ttdomain.Account, commentID string, hidden bool) (*ttdomain.Comment, error) {
	target, creds, err := uc.owned(ctx, account, commentID)
	if err != nil {
		return nil, err
	}
	if err := uc.d.Services.Comments.SetHidden(ctx, creds, target.VideoID, target.CommentID, hidden); err != nil {
		return nil, err
	}
	visibility := ttdomain.VisibilityPublic
	if hidden {
		visibility = ttdomain.VisibilityHidden
	} else if confirmed := uc.confirmVisibility(ctx, creds, target); confirmed != ttdomain.VisibilityUnknown {
		visibility = confirmed
	}
	if err := uc.d.Comments.SetVisibility(ctx, account.ID, target.CommentID, visibility, uc.now()); err != nil {
		return nil, err
	}
	target.Visibility = visibility
	return target, nil
}

func (uc *Comments) confirmVisibility(ctx context.Context, creds ttdomain.Credentials, target *ttdomain.Comment) ttdomain.CommentVisibility {
	found, err := uc.d.Services.Comments.LookupComments(ctx, creds, target.VideoID, []string{target.CommentID})
	if err != nil || len(found) == 0 {
		return ttdomain.VisibilityUnknown
	}
	return found[0].Visibility
}

func (uc *Comments) SetLiked(ctx context.Context, workspaceID, accountID, commentID string, liked bool) (*ttdomain.Comment, error) {
	account, err := uc.readable(ctx, workspaceID, accountID, func(a *ttdomain.Account) bool { return a.CanManageCommentsNow() })
	if err != nil {
		return nil, err
	}
	return uc.setLiked(ctx, account, commentID, liked)
}

func (uc *Comments) setLiked(ctx context.Context, account *ttdomain.Account, commentID string, liked bool) (*ttdomain.Comment, error) {
	target, creds, err := uc.owned(ctx, account, commentID)
	if err != nil {
		return nil, err
	}
	if err := uc.d.Services.Comments.SetLiked(ctx, creds, target.CommentID, liked); err != nil {
		return nil, err
	}
	if err := uc.d.Comments.SetLiked(ctx, account.ID, target.CommentID, liked); err != nil {
		return nil, err
	}
	target.LikedByUs = liked
	return target, nil
}
