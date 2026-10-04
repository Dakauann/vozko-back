package advertising

import (
	"context"
	"fmt"
	"sort"

	ads "vozko/domain/advertising"
)

type commentsGateway interface {
	GetAdPosts(ctx context.Context, token, adMetaID string) (ads.AdPosts, error)
	ListPostComments(ctx context.Context, token, pageID, postID string) ([]ads.AdComment, error)
	ListInstagramComments(ctx context.Context, token, mediaID string) ([]ads.AdComment, error)
}

type CommentsUseCase struct {
	access  accountAccess
	objects ads.ObjectRepository
	gateway commentsGateway
}

func NewCommentsUseCase(sync *SyncUseCase, gateway commentsGateway) *CommentsUseCase {
	return &CommentsUseCase{access: sync.access, objects: sync.objects, gateway: gateway}
}

func (uc *CommentsUseCase) List(ctx context.Context, workspaceID, metaID, platform string) ([]ads.AdComment, error) {
	channel, err := ads.CommentPlatformOf(platform)
	if err != nil {
		return nil, err
	}
	t, err := openTarget(ctx, uc.access, uc.objects, workspaceID, metaID, ads.UseRead)
	if err != nil {
		return nil, err
	}
	if err := t.object.CommentsAllowed(); err != nil {
		return nil, err
	}
	posts, err := uc.gateway.GetAdPosts(ctx, t.token, metaID)
	if err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	postID, err := posts.PostOn(channel)
	if err != nil {
		return nil, err
	}
	comments, err := uc.read(ctx, t.token, channel, posts.PageID(), postID)
	if ads.Classify(err) == ads.FailurePermission {
		return nil, fmt.Errorf("%w: %w", ads.ErrCommentsNotAllowed, err)
	}
	if err != nil {
		return nil, uc.access.failed(ctx, t.account, err)
	}
	return newestFirst(comments), nil
}

func (uc *CommentsUseCase) read(ctx context.Context, token, channel, pageID, postID string) ([]ads.AdComment, error) {
	if channel == ads.PlatformInstagram {
		return uc.gateway.ListInstagramComments(ctx, token, postID)
	}
	return uc.gateway.ListPostComments(ctx, token, pageID, postID)
}

func newestFirst(comments []ads.AdComment) []ads.AdComment {
	sort.SliceStable(comments, func(i, j int) bool {
		a, b := comments[i].CreatedAt, comments[j].CreatedAt
		return a != nil && (b == nil || a.After(*b))
	})
	if len(comments) > ads.MaxAdComments {
		return comments[:ads.MaxAdComments]
	}
	return comments
}
