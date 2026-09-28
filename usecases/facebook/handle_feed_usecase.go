package facebook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	fbdomain "vozko/domain/facebook"
	mm "vozko/domain/metamessaging"
)

type CommentRuleRunner interface {
	Evaluate(ctx context.Context, page *fbdomain.Page, comment *fbdomain.Comment)
}

type CommentAudience interface {
	Enqueue(ctx context.Context, comment *fbdomain.Comment)
	Forget(ctx context.Context, fbCommentID string)
}

type VideoResolver interface {
	ResolveVideo(ctx context.Context, videoID, state string) error
}

type HandleFeedDeps struct {
	Videos   VideoResolver
	Pages    fbdomain.PageRepository
	Posts    fbdomain.PostRepository
	Comments fbdomain.CommentRepository
	Contacts fbdomain.ContactRepository
	Service  fbdomain.CommentService
	Rules    CommentRuleRunner
	Audience CommentAudience
}

type HandleFeedUseCase struct {
	d   HandleFeedDeps
	now func() time.Time
}

func NewHandleFeedUseCase(d HandleFeedDeps) *HandleFeedUseCase {
	return &HandleFeedUseCase{d: d, now: func() time.Time { return time.Now().UTC() }}
}

var feedItemKinds = map[string]fbdomain.PostKind{
	"status": fbdomain.PostStatus, "post": fbdomain.PostStatus, "photo": fbdomain.PostPhoto,
	"video": fbdomain.PostVideo, "share": fbdomain.PostLink, "link": fbdomain.PostLink, "album": fbdomain.PostAlbum,
}

func (uc *HandleFeedUseCase) Execute(ctx context.Context, env *mm.EntryEnvelope) error {
	if env == nil || env.Entry == nil || len(env.Entry.Changes) == 0 {
		return nil
	}
	page, err := uc.d.Pages.FindByFBPageID(ctx, env.Entry.ID)
	if errors.Is(err, fbdomain.ErrPageNotFound) {
		return fmt.Errorf("%w: %s", ErrUnknownPage, env.Entry.ID)
	}
	if err != nil {
		return err
	}
	if page.Status == fbdomain.StatusDisconnected {
		return nil
	}

	var errs []error
	for _, change := range env.Entry.Changes {
		if change == nil {
			continue
		}
		ev, err := fbdomain.NormalizeFeedChange(change.Field, change.Value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := uc.handle(ctx, page, ev); err != nil {
			log.Printf("[facebook-feed] %s on page %s failed: %v", ev.Kind, page.FBPageID, err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (uc *HandleFeedUseCase) handle(ctx context.Context, page *fbdomain.Page, ev *fbdomain.FeedEvent) error {
	switch ev.Kind {
	case fbdomain.FeedPostAdded, fbdomain.FeedPostEdited, fbdomain.FeedPostRemoved, fbdomain.FeedPostHidden, fbdomain.FeedPostUnhidden:
		if !page.OwnsPostID(ev.PostID) {
			return nil
		}
		return uc.post(ctx, page, ev)
	case fbdomain.FeedCommentAdded, fbdomain.FeedCommentEdited, fbdomain.FeedCommentRemoved, fbdomain.FeedCommentHidden, fbdomain.FeedCommentUnhidden:
		if !page.OwnsPostID(ev.PostID) {
			return nil
		}
		return uc.comment(ctx, page, ev)
	case fbdomain.FeedReaction:
		if !page.OwnsPostID(ev.PostID) {
			return nil
		}
		return uc.reaction(ctx, ev)
	case fbdomain.FeedVideoStatus:
		return uc.d.Videos.ResolveVideo(ctx, ev.VideoID, ev.VideoStatus)
	case fbdomain.FeedUnknown:
		log.Printf("[facebook-feed] unhandled change on page %s: %s", page.FBPageID, truncate(ev.Raw, 512))
	default:
		log.Printf("[facebook-feed] %s on page %s is not handled yet", ev.Kind, page.FBPageID)
	}
	return nil
}

func (uc *HandleFeedUseCase) post(ctx context.Context, page *fbdomain.Page, ev *fbdomain.FeedEvent) error {
	switch ev.Kind {
	case fbdomain.FeedPostAdded:
		kind := feedItemKinds[ev.Item]
		if !ev.IsFromPage(page.FBPageID) {
			kind = fbdomain.PostVisitor
		}
		return uc.d.Posts.Track(ctx, &fbdomain.Post{
			WorkspaceID: page.WorkspaceID, PageID: page.ID, FBPostID: ev.PostID, Kind: kind,
			Message: ev.Message, IsPublished: ev.Published == nil || *ev.Published, CreatedTime: timeOrNil(ev.CreatedTime),
		})
	case fbdomain.FeedPostEdited:
		if ev.Message == "" {
			return nil
		}
		return uc.d.Posts.UpdateMessage(ctx, ev.PostID, ev.Message)
	case fbdomain.FeedPostRemoved:
		return uc.d.Posts.Remove(ctx, ev.PostID)
	case fbdomain.FeedPostHidden:
		return uc.d.Posts.SetHidden(ctx, ev.PostID, true)
	case fbdomain.FeedPostUnhidden:
		return uc.d.Posts.SetHidden(ctx, ev.PostID, false)
	}
	return nil
}

func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (uc *HandleFeedUseCase) comment(ctx context.Context, page *fbdomain.Page, ev *fbdomain.FeedEvent) error {
	switch ev.Kind {
	case fbdomain.FeedCommentAdded:
		return uc.commentAdded(ctx, page, ev)
	case fbdomain.FeedCommentEdited:
		if err := uc.d.Comments.Edit(ctx, ev.CommentID, ev.Message, uc.now()); err != nil {
			return err
		}
		if stored, err := uc.d.Comments.FindByFBCommentID(ctx, ev.CommentID); err == nil && !stored.IsOurs {
			uc.d.Audience.Enqueue(ctx, stored)
		}
		return nil
	case fbdomain.FeedCommentRemoved:
		if err := uc.d.Comments.MarkRemoved(ctx, ev.CommentID, uc.now()); err != nil {
			return err
		}
		uc.d.Audience.Forget(ctx, ev.CommentID)
		return uc.d.Posts.AddCounts(ctx, ev.PostID, 0, -1)
	case fbdomain.FeedCommentHidden:
		return uc.d.Comments.SetHidden(ctx, ev.CommentID, true)
	case fbdomain.FeedCommentUnhidden:
		return uc.d.Comments.SetHidden(ctx, ev.CommentID, false)
	}
	return nil
}

func (uc *HandleFeedUseCase) commentAdded(ctx context.Context, page *fbdomain.Page, ev *fbdomain.FeedEvent) error {
	if ev.From == nil {
		uc.fillAuthor(ctx, page, ev)
	}
	c := page.CommentFromFeed(ev)
	if c.FromID != nil && !c.FromIsPage {
		contact, err := uc.d.Contacts.FindOrCreate(ctx, page.WorkspaceID, page.ID, *c.FromID)
		if err != nil {
			return err
		}
		c.ContactID = &contact.ID
	}
	if err := uc.d.Comments.UpsertMany(ctx, []*fbdomain.Comment{c}); err != nil {
		return err
	}
	if err := uc.d.Posts.AddCounts(ctx, ev.PostID, 0, 1); err != nil {
		log.Printf("[facebook-feed] post %s comment count not updated: %v", ev.PostID, err)
	}
	if c.IsOurs {
		return nil
	}
	uc.d.Rules.Evaluate(ctx, page, c)
	uc.d.Audience.Enqueue(ctx, c)
	return nil
}

func (uc *HandleFeedUseCase) fillAuthor(ctx context.Context, page *fbdomain.Page, ev *fbdomain.FeedEvent) {
	remote, err := uc.d.Service.Get(ctx, page.PageToken, ev.CommentID)
	if err != nil || remote.FromID == "" {
		log.Printf("[facebook-feed] comment %s author unknown: %v", ev.CommentID, err)
		return
	}
	ev.From = &fbdomain.Actor{ID: remote.FromID, Name: remote.FromName}
}

func (uc *HandleFeedUseCase) reaction(ctx context.Context, ev *fbdomain.FeedEvent) error {
	delta := int(fbdomain.ReactionDeltaFor(ev.Verb))
	if delta == 0 {
		return nil
	}
	if ev.CommentID != "" {
		return uc.d.Comments.AddLikes(ctx, ev.CommentID, delta)
	}
	return uc.d.Posts.AddCounts(ctx, ev.PostID, delta, 0)
}
