package facebook

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	ca "vozko/domain/audience"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
)

const backfillPageSize = 50

type AnalysisTombstoner interface {
	SoftDeleteBySourceComment(ctx context.Context, source ca.Source, sourceCommentID string, now time.Time) error
}

type AudienceDeps struct {
	Ingestor   ca.Ingestor
	Pages      fbdomain.PageRepository
	Posts      fbdomain.PostRepository
	Comments   fbdomain.CommentRepository
	Service    fbdomain.CommentService
	Tombstones AnalysisTombstoner
}

type AudienceAdapter struct {
	d     AudienceDeps
	clock shared.Clock
}

func NewAudienceAdapter(d AudienceDeps) *AudienceAdapter {
	return &AudienceAdapter{d: d, clock: shared.SystemClock{}}
}

var _ ca.SourceAdapter = (*AudienceAdapter)(nil)

func toIngestInput(c *fbdomain.Comment) (ca.IngestInput, bool) {
	if c == nil || c.FBCommentID == "" || c.FBPostID == "" || c.PageID == "" {
		return ca.IngestInput{}, false
	}
	in := ca.IngestInput{
		WorkspaceID:  c.WorkspaceID,
		Container:    ca.ContainerRef{Source: ca.SourceFacebook, AccountID: c.PageID, ContainerID: c.FBPostID},
		SubjectID:    c.FBCommentID,
		AuthorHandle: c.FromName,
		Text:         c.Message,
		IsOurs:       c.IsOurs,
	}
	if c.FromID != nil {
		in.AuthorExternalID = *c.FromID
	}
	if c.ParentFBCommentID != nil && *c.ParentFBCommentID != c.FBPostID {
		in.ParentSubjectID = *c.ParentFBCommentID
	}
	if c.CreatedTime != nil {
		in.OccurredAt = *c.CreatedTime
	}
	return in, true
}

func (a *AudienceAdapter) Enqueue(ctx context.Context, c *fbdomain.Comment) {
	in, ok := toIngestInput(c)
	if !ok {
		return
	}
	if err := a.d.Ingestor.Enqueue(ctx, in); err != nil {
		log.Printf("[facebook] comment analysis enqueue failed comment=%s: %v", c.FBCommentID, err)
	}
}

func (a *AudienceAdapter) Forget(ctx context.Context, fbCommentID string) {
	if strings.TrimSpace(fbCommentID) == "" {
		return
	}
	if err := a.d.Tombstones.SoftDeleteBySourceComment(ctx, ca.SourceFacebook, fbCommentID, a.clock.Now()); err != nil {
		log.Printf("[facebook] comment analysis tombstone failed comment=%s: %v", fbCommentID, err)
	}
}

func (a *AudienceAdapter) ReadTexts(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		c, err := a.d.Comments.FindByFBCommentID(ctx, id)
		if errors.Is(err, fbdomain.ErrCommentNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if c.PageID == ref.AccountID {
			out[id] = c.Message
		}
	}
	return out, nil
}

func (a *AudienceAdapter) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	post, err := a.d.Posts.FindByFBPostID(ctx, ref.ContainerID)
	if errors.Is(err, fbdomain.ErrPostNotFound) {
		return ca.ContainerContext{}, nil
	}
	if err != nil {
		return ca.ContainerContext{}, err
	}
	out := ca.ContainerContext{Caption: post.Message, Permalink: post.PermalinkURL, PublishedAt: post.CreatedTime}
	if page, err := a.d.Pages.FindByID(ctx, ref.AccountID); err == nil {
		out.AccountName = page.Name
	}
	return out, nil
}

func (a *AudienceAdapter) ListContainers(ctx context.Context, pageID string, limit, offset int) ([]ca.ContainerSummary, error) {
	posts, err := a.d.Posts.ListByPage(ctx, pageID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]ca.ContainerSummary, 0, len(posts))
	for _, p := range posts {
		out = append(out, ca.ContainerSummary{
			Ref:           ca.ContainerRef{Source: ca.SourceFacebook, AccountID: p.PageID, ContainerID: p.FBPostID},
			WorkspaceID:   p.WorkspaceID,
			CommentsCount: p.CommentsCount,
		})
	}
	return out, nil
}

func (a *AudienceAdapter) FetchCommentsPage(ctx context.Context, ref ca.ContainerRef, cursor string) ([]ca.IngestInput, string, error) {
	page, err := a.d.Pages.FindByID(ctx, ref.AccountID)
	if err != nil {
		return nil, "", err
	}
	remote, err := a.d.Service.List(ctx, page.PageToken, ref.ContainerID, fbdomain.CommentsStream, backfillPageSize, cursor)
	if err != nil {
		return nil, "", err
	}
	records := make([]*fbdomain.Comment, 0, len(remote.Items))
	inputs := make([]ca.IngestInput, 0, len(remote.Items))
	for _, rc := range remote.Items {
		c := fbdomain.CommentFromRemote(page, ref.ContainerID, rc)
		records = append(records, c)
		if in, ok := toIngestInput(c); ok {
			inputs = append(inputs, in)
		}
	}
	if err := a.d.Comments.UpsertMany(ctx, records); err != nil {
		return nil, "", err
	}
	next := ""
	if remote.HasNext {
		next = remote.NextCursor
	}
	return inputs, next, nil
}

type AudienceDisabled struct{}

func (AudienceDisabled) Enqueue(context.Context, *fbdomain.Comment) {}
func (AudienceDisabled) Forget(context.Context, string)             {}
