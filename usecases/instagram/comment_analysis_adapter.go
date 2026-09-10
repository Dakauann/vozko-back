package instagram

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	ca "vozko/domain/comment_analysis"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
)

// CommentAnalysisAdapter is Instagram's side of the comment-analysis engine:
// the ONE file that knows both this channel's tables and the engine's ports.
//
// Inbound, it is the CommentAnalysisEnqueuer the webhook calls. Outbound, it
// is the engine's SourceAdapter: it reads comment bodies back from
// instagram_comments at classification time (the engine never stores them),
// supplies the post's caption, enumerates posts for a backfill from the
// local projection, and pages the Graph comments edge when a backfill runs.

// analysisTombstoner is the one engine repository method the delete path
// needs; narrowed so the adapter does not hold the whole repository.
type analysisTombstoner interface {
	SoftDeleteBySourceComment(ctx context.Context, source ca.Source, sourceCommentID string, now time.Time) error
}

type CommentAnalysisAdapter struct {
	ingestor   ca.Ingestor
	comments   igdomain.CommentRepository
	media      igdomain.MediaRepository
	accounts   igdomain.AccountRepository
	commentSvc igdomain.CommentService
	tombstones analysisTombstoner
	clock      shared.Clock
}

// NewCommentAnalysisAdapter builds the adapter. accounts and commentSvc are
// only needed for backfill (they fetch off the Graph edge) and may be nil in
// a deployment that never backfills.
func NewCommentAnalysisAdapter(
	ingestor ca.Ingestor,
	comments igdomain.CommentRepository,
	media igdomain.MediaRepository,
	accounts igdomain.AccountRepository,
	commentSvc igdomain.CommentService,
	tombstones analysisTombstoner,
) *CommentAnalysisAdapter {
	return &CommentAnalysisAdapter{
		ingestor: ingestor, comments: comments, media: media, accounts: accounts,
		commentSvc: commentSvc, tombstones: tombstones, clock: shared.SystemClock{},
	}
}

var (
	_ CommentAnalysisEnqueuer = (*CommentAnalysisAdapter)(nil)
	_ ca.SourceAdapter        = (*CommentAnalysisAdapter)(nil)
)

// ---- inbound: CommentAnalysisEnqueuer ----

func toIngestInput(c *igdomain.Comment) (ca.IngestInput, bool) {
	if c == nil || c.IGCommentID == "" || c.IGMediaID == "" || c.IGAccountID == "" {
		return ca.IngestInput{}, false
	}
	in := ca.IngestInput{
		WorkspaceID:      c.WorkspaceID,
		Container:        ca.ContainerRef{Source: ca.SourceInstagram, AccountID: c.IGAccountID, ContainerID: c.IGMediaID},
		SourceCommentID:  c.IGCommentID,
		AuthorExternalID: c.FromIGSID,
		AuthorHandle:     c.FromUsername,
		Text:             c.Text,
		IsOurs:           c.IsOurs,
	}
	if c.ParentIGCommentID != nil {
		in.ParentCommentID = *c.ParentIGCommentID
	}
	if c.Timestamp != nil {
		in.CommentedAt = *c.Timestamp
	}
	return in, true
}

func (a *CommentAnalysisAdapter) Enqueue(ctx context.Context, c *igdomain.Comment) {
	in, ok := toIngestInput(c)
	if !ok {
		return
	}
	if err := a.ingestor.Enqueue(ctx, in); err != nil {
		// Best effort by contract: the webhook already stored the comment.
		log.Printf("[instagram] comment analysis enqueue failed comment=%s: %v", c.IGCommentID, err)
	}
}

func (a *CommentAnalysisAdapter) Forget(ctx context.Context, igCommentID string) {
	if a.tombstones == nil || strings.TrimSpace(igCommentID) == "" {
		return
	}
	if err := a.tombstones.SoftDeleteBySourceComment(ctx, ca.SourceInstagram, igCommentID, a.clock.Now()); err != nil {
		log.Printf("[instagram] comment analysis tombstone failed comment=%s: %v", igCommentID, err)
	}
}

// ---- outbound: ca.SourceAdapter ----

func (a *CommentAnalysisAdapter) ReadTexts(ctx context.Context, ref ca.ContainerRef, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		c, err := a.comments.FindByIGCommentID(ctx, ref.AccountID, id)
		if err != nil {
			if errors.Is(err, igdomain.ErrCommentNotFound) {
				continue // deleted since ingest: absent, the engine skips it
			}
			return nil, err
		}
		out[id] = c.Text
	}
	return out, nil
}

func (a *CommentAnalysisAdapter) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	m, err := a.media.FindByIGMediaID(ctx, ref.AccountID, ref.ContainerID)
	if err != nil {
		if errors.Is(err, igdomain.ErrMediaNotFound) {
			return ca.ContainerContext{}, nil
		}
		return ca.ContainerContext{}, err
	}
	out := ca.ContainerContext{Caption: m.Caption, Permalink: m.Permalink, PublishedAt: m.Timestamp}
	// The handle is best effort: it is only needed so an ALERT can name the
	// account to a human, and the classifier does not care. accounts is nil in
	// a deployment that never backfills, and a missing handle costs a line in
	// a message rather than an analysis.
	if a.accounts != nil {
		if account, err := a.accounts.FindByID(ctx, ref.AccountID); err == nil && account != nil {
			out.AccountName = account.Username
		}
	}
	return out, nil
}

func (a *CommentAnalysisAdapter) ListContainers(ctx context.Context, accountID string, limit, offset int) ([]ca.ContainerSummary, error) {
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

// FetchCommentsPage pulls one page off the Graph edge for a backfill,
// mirroring each comment locally on the way (the engine reads bodies back
// from the mirror) and returning the ingest inputs.
func (a *CommentAnalysisAdapter) FetchCommentsPage(ctx context.Context, ref ca.ContainerRef, cursor string) ([]ca.IngestInput, string, error) {
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

// flattenRemote mirrors a comment and its replies, the same shape the
// webhook produces, so a backfilled comment is indistinguishable from a
// live one downstream.
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

// ---- ownership ----

// commentAnalysisAccountVerifier answers "does this workspace own this
// account" from the account row, for the settings and backfill use cases.
type commentAnalysisAccountVerifier struct {
	accounts igdomain.AccountRepository
}

func NewCommentAnalysisAccountVerifier(accounts igdomain.AccountRepository) *commentAnalysisAccountVerifier {
	return &commentAnalysisAccountVerifier{accounts: accounts}
}

func (v *commentAnalysisAccountVerifier) AccountBelongsTo(ctx context.Context, workspaceID, accountID string) (bool, error) {
	account, err := v.accounts.FindByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, igdomain.ErrAccountNotFound) {
			return false, nil
		}
		return false, err
	}
	return account != nil && account.WorkspaceID == workspaceID, nil
}
