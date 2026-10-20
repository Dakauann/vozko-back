package tiktok

import (
	"context"
	"log"
	"time"

	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/pkg/bounded"
	commentautomation "vozko/usecases/commentautomation"
)

const (
	commentWorkLease       = 2 * time.Minute
	commentWorkBatch       = 300
	commentWorkConcurrency = 4
)

type RuleEvaluator interface {
	Evaluate(ctx context.Context, source shared.EntryType, accountID, commentID string, subject ca.Subject, actions commentautomation.Actions) error
}

type CommentAudience interface {
	Enqueue(ctx context.Context, comment *ttdomain.Comment)
}

type CommentWorkerDeps struct {
	Accounts ttdomain.AccountRepository
	Contacts ttdomain.ContactRepository
	Comments ttdomain.CommentRepository
	Tokens   *TokenSource
	Service  ttdomain.CommentService
	Errors   ttdomain.ErrorClassifier
	Rules    RuleEvaluator
	Actions  commentautomation.Actions
	Audience CommentAudience
	Metrics  Metrics
}

type CommentWorker struct {
	d   CommentWorkerDeps
	now Clock
}

func NewCommentWorker(d CommentWorkerDeps) *CommentWorker {
	if d.Accounts == nil || d.Contacts == nil || d.Comments == nil || d.Tokens == nil || d.Service == nil || d.Errors == nil ||
		d.Rules == nil || d.Actions == nil || d.Audience == nil || d.Metrics == nil {
		panic("tiktok: the comment worker requires accounts, contacts, comments, tokens, the comment service, an error classifier, rules, actions, the audience and metrics")
	}
	return &CommentWorker{d: d, now: utcNow}
}

type accountBatch struct {
	accountID string
	comments  []*ttdomain.Comment
}

func (w *CommentWorker) Execute(ctx context.Context) error {
	claimed, err := w.d.Comments.ClaimLookups(ctx, w.now(), commentWorkLease, commentWorkBatch)
	if err != nil {
		return err
	}
	if len(claimed) == 0 {
		return nil
	}
	byAccount := map[string]*accountBatch{}
	var batches []*accountBatch
	for _, c := range claimed {
		b, ok := byAccount[c.AccountID]
		if !ok {
			b = &accountBatch{accountID: c.AccountID}
			byAccount[c.AccountID] = b
			batches = append(batches, b)
		}
		b.comments = append(b.comments, c)
	}
	bounded.ForEach(ctx, batches, commentWorkConcurrency, func(ctx context.Context, b *accountBatch) {
		w.work(ttdomain.WithBackground(ctx), b)
	})
	return nil
}

func (w *CommentWorker) work(ctx context.Context, b *accountBatch) {
	account, err := w.d.Accounts.FindByID(ctx, b.accountID)
	if err != nil || !account.CanReadCommentsNow() {
		w.d.Metrics.AddTikTokEvent("comment_work", "account_unavailable")
		for _, c := range b.comments {
			w.retry(ctx, c)
		}
		return
	}
	pending := map[string][]*ttdomain.Comment{}
	var ready []*ttdomain.Comment
	for _, c := range b.comments {
		if c.Lookup == ttdomain.LookupPending {
			pending[c.VideoID] = append(pending[c.VideoID], c)
			continue
		}
		ready = append(ready, c)
	}
	for videoID, comments := range pending {
		for start := 0; start < len(comments); start += ttdomain.CommentLookupBatch {
			end := min(start+ttdomain.CommentLookupBatch, len(comments))
			ready = append(ready, w.lookup(ctx, account, videoID, comments[start:end])...)
		}
	}
	for _, c := range ready {
		w.runRules(ctx, account, c)
	}
}

func (w *CommentWorker) lookup(ctx context.Context, account *ttdomain.Account, videoID string, comments []*ttdomain.Comment) []*ttdomain.Comment {
	creds, err := w.d.Tokens.Credentials(ctx, account)
	if err != nil {
		for _, c := range comments {
			w.retry(ctx, c)
		}
		return nil
	}
	ids := make([]string, len(comments))
	for i, c := range comments {
		ids[i] = c.CommentID
	}
	found, err := w.d.Service.LookupComments(ctx, creds, videoID, ids)
	if err != nil {
		w.d.Metrics.AddTikTokEvent("comment_work", "lookup_failed")
		for _, c := range comments {
			w.retry(ctx, c)
		}
		return nil
	}
	byID := make(map[string]ttdomain.RemoteComment, len(found))
	for _, rc := range found {
		byID[rc.CommentID] = rc
	}
	var resolved []*ttdomain.Comment
	for _, c := range comments {
		rc, ok := byID[c.CommentID]
		if !ok {
			w.d.Metrics.AddTikTokEvent("comment_work", "not_returned")
			w.retry(ctx, c)
			continue
		}
		if r := w.resolve(ctx, account, c, rc); r != nil {
			resolved = append(resolved, r)
		}
	}
	return resolved
}

func (w *CommentWorker) resolve(ctx context.Context, account *ttdomain.Account, c *ttdomain.Comment, rc ttdomain.RemoteComment) *ttdomain.Comment {
	author := ttdomain.ResolvedAuthor{
		UniqueIdentifier: rc.UniqueIdentifier, Username: rc.Username, DisplayName: rc.DisplayName, Text: rc.Text, ImageURL: rc.ImageURL,
		Visibility: rc.Visibility, IsOurs: rc.Owner, Liked: rc.Liked, Likes: rc.Likes, Replies: rc.Replies, CommentedAt: rc.CreatedAt,
	}
	if rc.Owner {
		if rc.UniqueIdentifier != "" && account.UniqueIdentifier == "" {
			if err := w.d.Accounts.LearnUniqueIdentifier(ctx, account.ID, rc.UniqueIdentifier); err == nil {
				account.UniqueIdentifier = rc.UniqueIdentifier
			}
		}
	} else if rc.UniqueIdentifier != "" {
		contact, err := w.d.Contacts.FindOrCreate(ctx, ttdomain.FindOrCreateContactInput{WorkspaceID: account.WorkspaceID, AccountID: account.ID, UniqueIdentifier: rc.UniqueIdentifier, Username: rc.Username})
		if err != nil {
			w.retry(ctx, c)
			return nil
		}
		author.ContactID = &contact.ID
		if rc.DisplayName != "" && contact.DisplayName == "" {
			if err := w.d.Contacts.UpdateProfile(ctx, contact.ID, ttdomain.ContactProfile{Username: rc.Username, DisplayName: rc.DisplayName}, w.now()); err != nil {
				log.Printf("[tiktok][comments] profile of contact %s not saved: %v", contact.ID, err)
			}
		}
	}
	if err := w.d.Comments.ResolveLookup(ctx, c.ID, author); err != nil {
		w.retry(ctx, c)
		return nil
	}
	ours := rc.Owner
	c.IsOurs = &ours
	c.Lookup = ttdomain.LookupResolved
	c.AuthorUsername, c.AuthorDisplayName = rc.Username, rc.DisplayName
	if rc.Text != "" {
		c.Text = rc.Text
	}
	if rc.Visibility != ttdomain.VisibilityUnknown && c.Visibility != ttdomain.VisibilityDeleted {
		c.Visibility = rc.Visibility
	}
	if rc.CreatedAt != nil {
		c.CommentedAt = rc.CreatedAt
	}
	c.ContactID = author.ContactID
	if ours {
		c.RulesDue = false
	}
	w.d.Metrics.AddTikTokEvent("comment_work", "resolved")
	w.d.Audience.Enqueue(ctx, c)
	return c
}

func (w *CommentWorker) runRules(ctx context.Context, account *ttdomain.Account, c *ttdomain.Comment) {
	if !c.ReadyForRules() {
		if c.RulesDue {
			w.done(ctx, c)
		}
		return
	}
	subject := ca.Subject{
		ContainerID: c.VideoID,
		AuthorName:  c.AuthorName(),
		Text:        c.Text,
		IsOurs:      false,
		Hidden:      c.Visibility == ttdomain.VisibilityHidden,
	}
	err := w.d.Rules.Evaluate(ctx, shared.EntryTypeTikTok, account.ID, c.CommentID, subject, w.d.Actions)
	switch {
	case err == nil:
		w.done(ctx, c)
	case ca.Retryable(err):
		w.d.Metrics.AddTikTokEvent("comment_work", "rules_retry")
		w.retry(ctx, c)
	default:
		w.d.Metrics.AddTikTokEvent("comment_work", "rules_failed")
		log.Printf("[tiktok][comments] rules of comment %s failed after the claim, not retried: %v", c.CommentID, err)
		w.done(ctx, c)
	}
}

func (w *CommentWorker) done(ctx context.Context, c *ttdomain.Comment) {
	if err := w.d.Comments.MarkRulesDone(ctx, c.ID); err != nil {
		log.Printf("[tiktok][comments] comment %s not marked done, its lease will expire and it runs again under the claim: %v", c.CommentID, err)
	}
}

func (w *CommentWorker) retry(ctx context.Context, c *ttdomain.Comment) {
	attempts := c.LookupAttempts + 1
	if attempts >= ttdomain.MaxLookupAttempts {
		w.d.Metrics.AddTikTokEvent("comment_work", "gave_up")
		if err := w.d.Comments.FailLookup(ctx, c.ID, attempts); err != nil {
			log.Printf("[tiktok][comments] comment %s not marked failed: %v", c.CommentID, err)
		}
		return
	}
	if err := w.d.Comments.DeferLookup(ctx, c.ID, attempts, w.now().Add(ttdomain.LookupBackoff(attempts))); err != nil {
		log.Printf("[tiktok][comments] comment %s not deferred, its lease expires instead: %v", c.CommentID, err)
	}
}

var _ RuleEvaluator = (*commentautomation.Evaluator)(nil)
