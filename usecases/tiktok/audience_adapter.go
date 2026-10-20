package tiktok

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	ca "vozko/domain/audience"
	ttdomain "vozko/domain/tiktok"
)

type AudienceTombstoner interface {
	SoftDeleteBySourceComment(ctx context.Context, source ca.Source, sourceCommentID string, now time.Time) error
}

type AudienceAdapterDeps struct {
	Ingestor   ca.Ingestor
	Tombstones AudienceTombstoner
	Accounts   ttdomain.AccountRepository
	Videos     ttdomain.VideoRepository
	Comments   ttdomain.CommentRepository
	Tokens     *TokenSource
	Service    ttdomain.CommentService
	Metrics    Metrics
}

type AudienceAdapter struct {
	d   AudienceAdapterDeps
	now Clock
}

func NewAudienceAdapter(d AudienceAdapterDeps) *AudienceAdapter {
	if d.Ingestor == nil || d.Tombstones == nil || d.Accounts == nil || d.Videos == nil || d.Comments == nil || d.Tokens == nil || d.Service == nil || d.Metrics == nil {
		panic("tiktok: the audience adapter requires the ingestor, tombstones, accounts, videos, comments, tokens, the comment service and metrics")
	}
	return &AudienceAdapter{d: d, now: utcNow}
}

var (
	_ ca.SourceAdapter = (*AudienceAdapter)(nil)
	_ CommentAudience  = (*AudienceAdapter)(nil)
	_ CommentSink      = (*AudienceAdapter)(nil)
)

func ingestInput(c *ttdomain.Comment) (ca.IngestInput, bool) {
	if c == nil || c.CommentID == "" || c.VideoID == "" || c.AccountID == "" || c.Visibility == ttdomain.VisibilityDeleted {
		return ca.IngestInput{}, false
	}
	in := ca.IngestInput{
		WorkspaceID:      c.WorkspaceID,
		Container:        ca.ContainerRef{Source: ca.SourceTikTok, AccountID: c.AccountID, ContainerID: c.VideoID},
		SubjectID:        c.CommentID,
		ParentSubjectID:  c.ParentCommentID,
		AuthorExternalID: c.AuthorUniqueIdentifier,
		AuthorHandle:     c.AuthorName(),
		Text:             c.Text,
		IsOurs:           c.IsOurs != nil && *c.IsOurs,
	}
	if c.CommentedAt != nil {
		in.OccurredAt = *c.CommentedAt
	}
	return in, true
}

func (a *AudienceAdapter) Enqueue(ctx context.Context, c *ttdomain.Comment) {
	in, ok := ingestInput(c)
	if !ok {
		return
	}
	if err := a.d.Ingestor.Enqueue(ctx, in); err != nil {
		a.d.Metrics.AddTikTokEvent("audience", "enqueue_failed")
		log.Printf("[tiktok][audience] comment %s not enqueued: %v", c.CommentID, err)
	}
}

func (a *AudienceAdapter) Forget(ctx context.Context, account *ttdomain.Account, commentID string) {
	if strings.TrimSpace(commentID) == "" {
		return
	}
	if err := a.d.Tombstones.SoftDeleteBySourceComment(ctx, ca.SourceTikTok, commentID, a.now()); err != nil {
		a.d.Metrics.AddTikTokEvent("audience", "tombstone_failed")
		log.Printf("[tiktok][audience] comment %s not tombstoned: %v", commentID, err)
	}
}

func (a *AudienceAdapter) ReadTexts(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]string, error) {
	comments, err := a.d.Comments.FindByCommentIDs(ctx, ref.AccountID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(comments))
	for _, c := range comments {
		if c.Visibility != ttdomain.VisibilityDeleted {
			out[c.CommentID] = c.Text
		}
	}
	return out, nil
}

func (a *AudienceAdapter) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	out := ca.ContainerContext{}
	if account, err := a.d.Accounts.FindByID(ctx, ref.AccountID); err == nil {
		out.AccountName = account.Name()
	}
	video, err := a.d.Videos.FindByItemID(ctx, ref.AccountID, ref.ContainerID)
	if errors.Is(err, ttdomain.ErrVideoNotFound) {
		return out, nil
	}
	if err != nil {
		return ca.ContainerContext{}, err
	}
	out.Caption, out.Permalink, out.PublishedAt = video.Caption, video.ShareURL, video.PostedAt
	return out, nil
}

func (a *AudienceAdapter) ListContainers(ctx context.Context, accountID string, limit, offset int) ([]ca.ContainerSummary, error) {
	videos, err := a.d.Videos.ListSlice(ctx, accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]ca.ContainerSummary, 0, len(videos))
	for _, v := range videos {
		out = append(out, ca.ContainerSummary{
			Ref:           ca.ContainerRef{Source: ca.SourceTikTok, AccountID: v.AccountID, ContainerID: v.ItemID},
			WorkspaceID:   v.WorkspaceID,
			CommentsCount: int(v.Comments),
		})
	}
	return out, nil
}

func (a *AudienceAdapter) FetchCommentsPage(ctx context.Context, ref ca.ContainerRef, cursor string) ([]ca.IngestInput, string, error) {
	account, err := a.d.Accounts.FindByID(ctx, ref.AccountID)
	if err != nil {
		return nil, "", err
	}
	if !account.CanReadCommentsNow() {
		return nil, "", ErrAccountUnavailable
	}
	creds, err := a.d.Tokens.Credentials(ctx, account)
	if err != nil {
		return nil, "", err
	}
	var position int64
	if strings.TrimSpace(cursor) != "" {
		position, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || position < 0 {
			return nil, "", errors.New("tiktok: invalid comment cursor")
		}
	}
	page, err := a.d.Service.ListComments(ttdomain.WithBackground(ctx), creds, ttdomain.CommentQuery{VideoID: ref.ContainerID, Cursor: position, IncludeHidden: true, IncludeReplies: true})
	if err != nil {
		return nil, "", err
	}
	comments := make([]*ttdomain.Comment, 0, len(page.Comments))
	inputs := make([]ca.IngestInput, 0, len(page.Comments))
	for _, rc := range page.Comments {
		c := mirroredComment(account, rc)
		comments = append(comments, c)
		if in, ok := ingestInput(c); ok {
			inputs = append(inputs, in)
		}
	}
	if err := a.d.Comments.MirrorMany(ctx, comments); err != nil {
		return nil, "", err
	}
	next := ""
	if page.HasMore {
		next = strconv.FormatInt(page.Cursor, 10)
	}
	return inputs, next, nil
}
