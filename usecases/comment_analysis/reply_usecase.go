package comment_analysis_usecase

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/balance"
	ca "vozko/domain/comment_analysis"
)

// Answering a comment (§6).
//
// Two use cases, deliberately separate, because they are two different acts:
// drafting spends money on a model call and shows a person some words, and
// posting puts those words under a public post in the customer's name. Nothing
// re-drafts at send time, so what the operator read is what gets published.
//
// Both refuse before doing anything when the account's policy says off. That
// check lives here rather than in the handler because "this account does not
// reply" is a fact about the account, and a second caller must not be able to
// skip it.

type replyDeps struct {
	repo     ca.Repository
	settings ca.SettingsRepository
	adapters map[ca.Source]ca.SourceAdapter
	drafter  ca.ReplyDrafter
	repliers map[ca.Source]ca.CommentReplier
	// A draft is a model call, so it answers to the same balance floor the
	// comment pass does. Without this a workspace at zero could keep drafting.
	guard   balanceGuard
	batches ca.BatchRepository
	clock   ca.Clock
}

// ReplyDeps groups what the reply path needs. Every field is optional at the
// container level and checked at call time, so a deployment without a model or
// without a channel that can reply still serves every other route.
type ReplyDeps struct {
	Repo     ca.Repository
	Settings ca.SettingsRepository
	Adapters map[ca.Source]ca.SourceAdapter
	Drafter  ca.ReplyDrafter
	Repliers map[ca.Source]ca.CommentReplier
	Balance  balance.CachedBalanceChecker
	Batches  ca.BatchRepository
	Clock    ca.Clock
}

// NewReplyUseCases builds both halves off one dependency set, so a caller
// cannot wire the drafting half against one account store and the posting half
// against another.
func NewReplyUseCases(d ReplyDeps) (ca.SuggestCommentReplyUseCase, ca.PostCommentReplyUseCase) {
	shared := replyDeps{
		repo: d.Repo, settings: d.Settings, adapters: d.Adapters,
		drafter: d.Drafter, repliers: d.Repliers,
		guard: newBalanceGuard(d.Balance, "reply"), batches: d.Batches, clock: d.Clock,
	}
	return &suggestReplyUseCase{replyDeps: shared}, &postReplyUseCase{replyDeps: shared}
}

// load resolves the comment inside the caller's workspace and the settings of
// the account it belongs to. The workspace is the session's, so a comment id
// from elsewhere is not found rather than answerable.
func (d replyDeps) load(ctx context.Context, workspaceID, commentID string) (*ca.CommentAnalysis, *ca.Settings, error) {
	comment, err := d.repo.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(commentID))
	if err != nil {
		return nil, nil, err
	}
	settings, err := d.settings.Find(ctx, comment.Source, comment.AccountID)
	if err != nil {
		// An account nobody configured has the disabled defaults, which is
		// exactly the answer we want: no replying.
		fallback := ca.NewSettings(comment.WorkspaceID, comment.Source, comment.AccountID, ca.VerticalServices)
		settings = &fallback
	}
	settings.Normalize()
	return comment, settings, nil
}

// commentText reads the words back from the channel, which owns them; the
// engine stores only an excerpt. The excerpt is the fallback so a deleted or
// unreachable comment can still be answered from what we have.
func (d replyDeps) commentText(ctx context.Context, c *ca.CommentAnalysis) string {
	adapter, ok := d.adapters[c.Source]
	if ok && adapter != nil {
		ref := ca.ContainerRef{Source: c.Source, AccountID: c.AccountID, ContainerID: c.ContainerID}
		if texts, err := adapter.ReadTexts(ctx, ref, []string{c.SourceCommentID}); err == nil {
			if text := strings.TrimSpace(texts[c.SourceCommentID]); text != "" {
				return text
			}
		}
	}
	return c.Excerpt
}

func (d replyDeps) caption(ctx context.Context, c *ca.CommentAnalysis) string {
	adapter, ok := d.adapters[c.Source]
	if !ok || adapter == nil {
		return ""
	}
	container, err := adapter.ReadContainerContext(ctx, ca.ContainerRef{
		Source: c.Source, AccountID: c.AccountID, ContainerID: c.ContainerID,
	})
	if err != nil {
		return ""
	}
	return container.Caption
}

// ---- drafting ----

type suggestReplyUseCase struct{ replyDeps }

func (uc *suggestReplyUseCase) Execute(ctx context.Context, in ca.SuggestReplyInput) (*ca.ReplySuggestion, error) {
	if uc.drafter == nil {
		return nil, fmt.Errorf("%w: no model is configured to draft replies", ca.ErrInvalidFilter)
	}
	comment, settings, err := uc.load(ctx, in.WorkspaceID, in.CommentID)
	if err != nil {
		return nil, err
	}
	if !settings.ReplyPolicy.CanSuggest() {
		return nil, fmt.Errorf("%w: this account does not draft replies", ca.ErrInvalidFilter)
	}
	// Checked BEFORE the model call, so an empty balance costs nothing.
	if err := uc.guard.Allow(comment.WorkspaceID); err != nil {
		return nil, err
	}

	text := uc.commentText(ctx, comment)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: there is nothing to answer", ca.ErrInvalidFilter)
	}

	result, err := uc.drafter.Draft(ctx, ca.ReplyDraftRequest{
		WorkspaceID:  comment.WorkspaceID,
		Model:        settings.Model,
		Instructions: settings.Instructions,
		Caption:      uc.caption(ctx, comment),
		Comment:      text,
		AuthorHandle: comment.AuthorHandle,
		Intent:       comment.Intent,
		Stance:       comment.Stance,
		Sentiment:    string(comment.Sentiment),
		Language:     comment.Language,
		MaxLength:    ca.MaxReplyLength,
	})
	if err != nil {
		return nil, err
	}

	// Booked like every other model call, under its own kind, so /spend shows
	// what drafting costs rather than hiding it inside the comment pass.
	recordAICall(ctx, uc.batches, uc.clock, aiCall{
		WorkspaceID: comment.WorkspaceID, Source: comment.Source, AccountID: comment.AccountID,
		ContainerID: comment.ContainerID, Kind: ca.BatchKindReply, Model: result.Model,
		PromptTokens: result.PromptTokens, CompletionTokens: result.CompletionTokens,
	})

	suggestion := ca.NewReplySuggestion(comment.ID, result.Text, result.Model)
	if err := suggestion.Validate(); err != nil {
		return nil, err
	}
	return &suggestion, nil
}

// ---- posting ----

type postReplyUseCase struct{ replyDeps }

func (uc *postReplyUseCase) Execute(ctx context.Context, in ca.PostReplyInput) (*ca.ReplySuggestion, error) {
	comment, settings, err := uc.load(ctx, in.WorkspaceID, in.CommentID)
	if err != nil {
		return nil, err
	}
	// Off means off, including for a human pressing send from this screen: an
	// account that has not turned replying on has not agreed to reply from
	// here at all. The channel's own reply route is unaffected.
	if !settings.ReplyPolicy.CanSuggest() {
		return nil, fmt.Errorf("%w: this account does not reply from the comment analysis", ca.ErrInvalidFilter)
	}

	replier, ok := uc.repliers[comment.Source]
	if !ok || replier == nil {
		return nil, fmt.Errorf("%w: this channel cannot post replies", ca.ErrInvalidFilter)
	}

	// The text is bounded and trimmed by the same value object that bounds a
	// draft, so an operator cannot post something a draft could not have been.
	suggestion := ca.NewReplySuggestion(comment.ID, in.Text, "")
	if err := suggestion.Validate(); err != nil {
		return nil, err
	}

	if _, err := replier.ReplyToComment(ctx, comment.WorkspaceID, comment.AccountID, comment.SourceCommentID, suggestion.Text); err != nil {
		return nil, err
	}
	return &suggestion, nil
}
