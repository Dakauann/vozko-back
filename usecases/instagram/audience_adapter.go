package instagram

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	ca "vozko/domain/audience"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

type analysisTombstoner interface {
	SoftDeleteBySourceComment(ctx context.Context, source ca.Source, sourceCommentID string, now time.Time) error
}

type AudienceAdapter struct {
	ingestor   ca.Ingestor
	comments   igdomain.CommentRepository
	media      igdomain.MediaRepository
	accounts   igdomain.AccountRepository
	commentSvc igdomain.CommentService
	tombstones analysisTombstoner
	clock      shared.Clock
}

func NewAudienceAdapter(
	ingestor ca.Ingestor,
	comments igdomain.CommentRepository,
	media igdomain.MediaRepository,
	accounts igdomain.AccountRepository,
	commentSvc igdomain.CommentService,
	tombstones analysisTombstoner,
) *AudienceAdapter {
	return &AudienceAdapter{
		ingestor: ingestor, comments: comments, media: media, accounts: accounts,
		commentSvc: commentSvc, tombstones: tombstones, clock: shared.SystemClock{},
	}
}

var (
	_ AudienceEnqueuer = (*AudienceAdapter)(nil)
	_ ca.SourceAdapter = (*AudienceAdapter)(nil)
)

func toIngestInput(c *igdomain.Comment) (ca.IngestInput, bool) {
	if c == nil || c.IGCommentID == "" || c.IGMediaID == "" || c.IGAccountID == "" {
		return ca.IngestInput{}, false
	}
	in := ca.IngestInput{
		WorkspaceID:      c.WorkspaceID,
		Container:        ca.ContainerRef{Source: ca.SourceInstagram, AccountID: c.IGAccountID, ContainerID: c.IGMediaID},
		SubjectID:        c.IGCommentID,
		AuthorExternalID: c.FromIGSID,
		AuthorHandle:     c.FromUsername,
		Text:             c.Text,
		IsOurs:           c.IsOurs,
	}
	if c.ParentIGCommentID != nil {
		in.ParentSubjectID = *c.ParentIGCommentID
	}
	if c.Timestamp != nil {
		in.OccurredAt = *c.Timestamp
	}
	return in, true
}

func (a *AudienceAdapter) Enqueue(ctx context.Context, c *igdomain.Comment) {
	in, ok := toIngestInput(c)
	if !ok {
		return
	}
	if err := a.ingestor.Enqueue(ctx, in); err != nil {
		log.Printf("[instagram] comment analysis enqueue failed comment=%s: %v", c.IGCommentID, err)
	}
}

func (a *AudienceAdapter) Forget(ctx context.Context, igCommentID string) {
	if a.tombstones == nil || strings.TrimSpace(igCommentID) == "" {
		return
	}
	if err := a.tombstones.SoftDeleteBySourceComment(ctx, ca.SourceInstagram, igCommentID, a.clock.Now()); err != nil {
		log.Printf("[instagram] comment analysis tombstone failed comment=%s: %v", igCommentID, err)
	}
}

func (a *AudienceAdapter) ReadTexts(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		c, err := a.comments.FindByIGCommentID(ctx, ref.AccountID, id)
		if err != nil {
			if errors.Is(err, igdomain.ErrCommentNotFound) {
				continue
			}
			return nil, err
		}
		out[id] = c.Text
	}
	return out, nil
}

func (a *AudienceAdapter) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	m, err := a.media.FindByIGMediaID(ctx, ref.AccountID, ref.ContainerID)
	if err != nil {
		if errors.Is(err, igdomain.ErrMediaNotFound) {
			return ca.ContainerContext{}, nil
		}
		return ca.ContainerContext{}, err
	}
	out := ca.ContainerContext{Caption: m.Caption, Permalink: m.Permalink, PublishedAt: m.Timestamp}
	if a.accounts != nil {
		if account, err := a.accounts.FindByID(ctx, ref.AccountID); err == nil && account != nil {
			out.AccountName = account.Username
		}
	}
	return out, nil
}

func (a *AudienceAdapter) ListContainers(ctx context.Context, accountID string, limit, offset int) ([]ca.ContainerSummary, error) {
	items, err := a.media.ListByAccount(ctx, accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]ca.ContainerSummary, 0, len(items))
	for _, m := range items {
		out = append(out, ca.ContainerSummary{
			Ref:           ca.ContainerRef{Source: ca.SourceInstagram, AccountID: m.IGAccountID, ContainerID: m.IGMediaID},
			WorkspaceID:   m.WorkspaceID,
			CommentsCount: m.CommentsCount,
		})
	}
	return out, nil
}

func (a *AudienceAdapter) FetchCommentsPage(ctx context.Context, ref ca.ContainerRef, cursor string) ([]ca.IngestInput, string, error) {
	if a.accounts == nil || a.commentSvc == nil {
		return nil, "", errors.New("instagram: backfill is not configured for this deployment")
	}
	account, err := a.accounts.FindByID(ctx, ref.AccountID)
	if err != nil {
		return nil, "", err
	}
	page, err := a.commentSvc.ListComments(ctx, account.AccessToken, ref.ContainerID, 50, cursor)
	if err != nil {
		return nil, "", err
	}
	records := make([]*igdomain.Comment, 0, len(page.Items))
	inputs := make([]ca.IngestInput, 0, len(page.Items))
	for _, rc := range page.Items {
		flattenRemote(account, ref.ContainerID, rc, &records, &inputs)
	}
	if len(records) > 0 {
		if err := a.comments.UpsertMany(ctx, records); err != nil {
			return nil, "", err
		}
	}
	next := ""
	if page.HasNext {
		next = page.NextCursor
	}
	return inputs, next, nil
}

func flattenRemote(account *igdomain.Account, igMediaID string, rc *igdomain.RemoteComment, records *[]*igdomain.Comment, inputs *[]ca.IngestInput) {
	if rc == nil || rc.IGCommentID == "" {
		return
	}
	c := &igdomain.Comment{
		WorkspaceID:  account.WorkspaceID,
		IGAccountID:  account.ID,
		IGCommentID:  rc.IGCommentID,
		IGMediaID:    igMediaID,
		FromIGSID:    rc.FromIGSID,
		FromUsername: firstNonEmpty(rc.FromUsername, rc.Username),
		Text:         rc.Text,
		LikeCount:    rc.LikeCount,
		Hidden:       rc.Hidden,
		IsOurs:       rc.IsOurs || (rc.FromIGSID != "" && rc.FromIGSID == account.IGUserID),
		Timestamp:    rc.Timestamp,
	}
	if rc.ParentID != "" {
		parent := rc.ParentID
		c.ParentIGCommentID = &parent
	}
	*records = append(*records, c)
	if in, ok := toIngestInput(c); ok {
		*inputs = append(*inputs, in)
	}
	for _, reply := range rc.Replies {
		if reply != nil && reply.ParentID == "" {
			reply.ParentID = rc.IGCommentID
		}
		flattenRemote(account, igMediaID, reply, records, inputs)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type audienceAccountVerifier struct {
	accounts igdomain.AccountRepository
}

func NewAudienceAccountVerifier(accounts igdomain.AccountRepository) *audienceAccountVerifier {
	return &audienceAccountVerifier{accounts: accounts}
}

func (v *audienceAccountVerifier) AccountBelongsTo(ctx context.Context, workspaceID, accountID string) (bool, error) {
	account, err := v.accounts.FindByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, igdomain.ErrAccountNotFound) {
			return false, nil
		}
		return false, err
	}
	return account != nil && account.WorkspaceID == workspaceID, nil
}
