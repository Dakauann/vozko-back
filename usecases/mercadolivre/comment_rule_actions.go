package mercadolivre

import (
	"context"
	"fmt"

	ca "vozko/domain/commentautomation"
	mldomain "vozko/domain/mercadolivre"
	cauc "vozko/usecases/commentautomation"
)

type CommentActions struct {
	questions *Questions
}

var _ cauc.Actions = CommentActions{}

func NewCommentActions(questions *Questions) CommentActions {
	if questions == nil {
		panic("mercadolivre: comment rule actions require the questions use case")
	}
	return CommentActions{questions: questions}
}

func (a CommentActions) ReplyPublicly(ctx context.Context, rule *ca.Rule, questionID, text string) error {
	ruleID := rule.ID
	_, err := a.questions.Answer(ctx, AnswerInput{
		WorkspaceID: rule.WorkspaceID, AccountID: rule.AccountID, QuestionID: questionID, Text: text,
		Source: mldomain.AnsweredByRule, RuleID: &ruleID,
	})
	return err
}

func (a CommentActions) Delete(ctx context.Context, rule *ca.Rule, questionID string) error {
	return a.questions.delete(ctx, rule.WorkspaceID, rule.AccountID, questionID, "rule")
}

func (a CommentActions) ReplyPrivately(context.Context, *ca.Rule, string, string) error {
	return fmt.Errorf("%w: private reply on mercadolivre", ca.ErrActionUnsupported)
}

func (a CommentActions) Hide(context.Context, *ca.Rule, string) error {
	return fmt.Errorf("%w: hide on mercadolivre", ca.ErrActionUnsupported)
}

func (a CommentActions) Like(context.Context, *ca.Rule, string) error {
	return fmt.Errorf("%w: like on mercadolivre", ca.ErrActionUnsupported)
}

type AccountOwnership struct {
	accounts mldomain.AccountRepository
}

func NewAccountOwnership(accounts mldomain.AccountRepository) AccountOwnership {
	if accounts == nil {
		panic("mercadolivre: the rule account check requires accounts")
	}
	return AccountOwnership{accounts: accounts}
}

func (o AccountOwnership) VerifyAccount(ctx context.Context, workspaceID, accountID string) error {
	_, err := o.accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	return err
}
