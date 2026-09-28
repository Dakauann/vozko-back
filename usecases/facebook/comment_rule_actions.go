package facebook

import (
	"context"
	"errors"

	ca "vozko/domain/commentautomation"
	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
)

type CommentActions struct {
	uc *CommentUseCases
}

func NewCommentActions(uc *CommentUseCases) CommentActions {
	return CommentActions{uc: uc}
}

func (a CommentActions) ReplyPublicly(ctx context.Context, rule *ca.Rule, fbCommentID, text string) error {
	_, err := a.uc.Reply(ctx, rule.WorkspaceID, rule.AccountID, fbCommentID, text)
	return err
}

func (a CommentActions) ReplyPrivately(ctx context.Context, rule *ca.Rule, fbCommentID, text string) error {
	_, err := a.uc.SendPrivateReply(ctx, rule.WorkspaceID, rule.AccountID, fbCommentID, conversation.SentBySystem(), text)
	return err
}

func (a CommentActions) Hide(ctx context.Context, rule *ca.Rule, fbCommentID string) error {
	return a.uc.SetHidden(ctx, rule.WorkspaceID, rule.AccountID, fbCommentID, true)
}

func (a CommentActions) Delete(ctx context.Context, rule *ca.Rule, fbCommentID string) error {
	return a.uc.Delete(ctx, rule.WorkspaceID, rule.AccountID, fbCommentID)
}

func (a CommentActions) Like(ctx context.Context, rule *ca.Rule, fbCommentID string) error {
	return a.uc.SetLiked(ctx, rule.WorkspaceID, rule.AccountID, fbCommentID, true)
}

type CommentRules struct {
	evaluator *cauc.Evaluator
	actions   cauc.Actions
}

func NewCommentRules(evaluator *cauc.Evaluator, actions cauc.Actions) *CommentRules {
	return &CommentRules{evaluator: evaluator, actions: actions}
}

func (r *CommentRules) Evaluate(ctx context.Context, page *fbdomain.Page, comment *fbdomain.Comment) {
	r.evaluator.Evaluate(ctx, shared.EntryTypeFacebook, page.ID, comment.FBCommentID, CommentSubject(comment), r.actions)
}

func CommentSubject(c *fbdomain.Comment) ca.Subject {
	return ca.Subject{ContainerID: c.FBPostID, AuthorName: c.FromName, Text: c.Message, IsOurs: c.IsOurs, Hidden: c.IsHidden}
}

type PageOwnership struct {
	pages fbdomain.PageRepository
}

func NewPageOwnership(pages fbdomain.PageRepository) PageOwnership {
	return PageOwnership{pages: pages}
}

func (o PageOwnership) VerifyAccount(ctx context.Context, workspaceID, pageID string) error {
	_, err := pageWith(ctx, o.pages, workspaceID, pageID, "")
	return err
}

func (o PageOwnership) AccountBelongsTo(ctx context.Context, workspaceID, pageID string) (bool, error) {
	err := o.VerifyAccount(ctx, workspaceID, pageID)
	if errors.Is(err, fbdomain.ErrPageNotFound) {
		return false, nil
	}
	return err == nil, err
}
