package instagram

import (
	"context"
	"fmt"

	ca "vozko/domain/commentautomation"
	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
)

type CommentRules struct {
	evaluator *cauc.Evaluator
	actions   *CommentActionRunner
}

func NewCommentRules(evaluator *cauc.Evaluator, actions *CommentActionRunner) *CommentRules {
	return &CommentRules{evaluator: evaluator, actions: actions}
}

func (r *CommentRules) Execute(ctx context.Context, comment *igdomain.Comment) {
	r.evaluator.Evaluate(ctx, shared.EntryTypeInstagram, comment.IGAccountID, comment.IGCommentID, CommentSubject(comment), r.actions)
}

func CommentSubject(c *igdomain.Comment) ca.Subject {
	return ca.Subject{
		ContainerID: c.IGMediaID,
		AuthorName:  c.FromUsername,
		Text:        c.Text,
		IsOurs:      c.IsOurs,
		Hidden:      c.Hidden,
	}
}

type AccountOwnership struct {
	accounts igdomain.AccountRepository
}

func NewAccountOwnership(accounts igdomain.AccountRepository) AccountOwnership {
	return AccountOwnership{accounts: accounts}
}

func (o AccountOwnership) VerifyAccount(ctx context.Context, workspaceID, accountID string) error {
	account, err := o.accounts.FindByID(ctx, accountID)
	if err != nil {
		return err
	}
	if account.WorkspaceID != workspaceID {
		return igdomain.ErrAccountNotFound
	}
	return nil
}

type CommentActionRunner struct {
	reply    *ReplyToCommentUseCase
	private  *SendPrivateReplyUseCase
	moderate *ModerateCommentUseCase
}

func NewCommentActionRunner(reply *ReplyToCommentUseCase, private *SendPrivateReplyUseCase, moderate *ModerateCommentUseCase) *CommentActionRunner {
	return &CommentActionRunner{reply: reply, private: private, moderate: moderate}
}

func (r *CommentActionRunner) ReplyPublicly(ctx context.Context, rule *ca.Rule, igCommentID, message string) error {
	_, err := r.reply.Execute(ctx, rule.WorkspaceID, rule.AccountID, igCommentID, message)
	return err
}

func (r *CommentActionRunner) ReplyPrivately(ctx context.Context, rule *ca.Rule, igCommentID, text string) error {
	return r.private.Execute(ctx, rule.WorkspaceID, rule.AccountID, igCommentID, conversation.SentBySystem(), text)
}

func (r *CommentActionRunner) Hide(ctx context.Context, rule *ca.Rule, igCommentID string) error {
	return r.moderate.SetHidden(ctx, rule.WorkspaceID, rule.AccountID, igCommentID, true)
}

func (r *CommentActionRunner) Delete(context.Context, *ca.Rule, string) error {
	return fmt.Errorf("%w: delete on instagram", ca.ErrActionUnsupported)
}

func (r *CommentActionRunner) Like(context.Context, *ca.Rule, string) error {
	return fmt.Errorf("%w: like on instagram", ca.ErrActionUnsupported)
}
