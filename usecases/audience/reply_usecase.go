package audience_usecase

import (
	"context"
	"fmt"
	"strings"

	ca "vozko/domain/audience"
	"vozko/domain/balance"
)

type replyDeps struct {
	repo     ca.Repository
	settings ca.SettingsRepository
	adapters map[ca.Source]ca.SourceAdapter
	drafter  ca.ReplyDrafter
	repliers map[ca.Source]ca.CommentReplier
	guard    balanceGuard
	batches  ca.BatchRepository
	clock    ca.Clock
}

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

func NewReplyUseCases(d ReplyDeps) (ca.SuggestCommentReplyUseCase, ca.PostCommentReplyUseCase) {
	shared := replyDeps{
		repo: d.Repo, settings: d.Settings, adapters: d.Adapters,
		drafter: d.Drafter, repliers: d.Repliers,
		guard: newBalanceGuard(d.Balance, "reply"), batches: d.Batches, clock: d.Clock,
	}
	return &suggestReplyUseCase{replyDeps: shared}, &postReplyUseCase{replyDeps: shared}
}

func (d replyDeps) load(ctx context.Context, workspaceID, commentID string) (*ca.Analysis, *ca.Settings, error) {
	comment, err := d.repo.FindByID(ctx, strings.TrimSpace(workspaceID), strings.TrimSpace(commentID))
	if err != nil {
		return nil, nil, err
	}
	settings, err := d.settings.Find(ctx, comment.Source, comment.AccountID)
	if err != nil {
		fallback := ca.NewSettings(comment.WorkspaceID, comment.Source, comment.AccountID, ca.VerticalServices)
		settings = &fallback
	}
	settings.Normalize()
	return comment, settings, nil
}

func (d replyDeps) commentText(ctx context.Context, c *ca.Analysis) string {
	adapter, ok := d.adapters[c.Source]
	if ok && adapter != nil {
		ref := ca.ContainerRef{Source: c.Source, AccountID: c.AccountID, ContainerID: c.ContainerID}
		if texts, err := adapter.ReadTexts(ctx, ref, []string{c.SubjectID}); err == nil {
			if text := strings.TrimSpace(texts[c.SubjectID]); text != "" {
				return text
			}
		}
	}
	return c.Excerpt
}

func (d replyDeps) caption(ctx context.Context, c *ca.Analysis) string {
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

type postReplyUseCase struct{ replyDeps }

func (uc *postReplyUseCase) Execute(ctx context.Context, in ca.PostReplyInput) (*ca.ReplySuggestion, error) {
	comment, settings, err := uc.load(ctx, in.WorkspaceID, in.CommentID)
	if err != nil {
		return nil, err
	}
	if !settings.ReplyPolicy.CanSuggest() {
		return nil, fmt.Errorf("%w: this account does not reply from the comment analysis", ca.ErrInvalidFilter)
	}

	replier, ok := uc.repliers[comment.Source]
	if !ok || replier == nil {
		return nil, fmt.Errorf("%w: this channel cannot post replies", ca.ErrInvalidFilter)
	}

	suggestion := ca.NewReplySuggestion(comment.ID, in.Text, "")
	if err := suggestion.Validate(); err != nil {
		return nil, err
	}

	if _, err := replier.ReplyToComment(ctx, comment.WorkspaceID, comment.AccountID, comment.SubjectID, suggestion.Text); err != nil {
		return nil, err
	}
	return &suggestion, nil
}
