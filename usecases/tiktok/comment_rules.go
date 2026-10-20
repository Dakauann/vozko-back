package tiktok

import (
	"context"
	"errors"
	"fmt"

	ca "vozko/domain/commentautomation"
	ttdomain "vozko/domain/tiktok"
	commentautomation "vozko/usecases/commentautomation"
)

type RuleActions struct {
	accounts ttdomain.AccountRepository
	comments *Comments
}

func NewRuleActions(accounts ttdomain.AccountRepository, comments *Comments) *RuleActions {
	if accounts == nil || comments == nil {
		panic("tiktok: rule actions require accounts and the comment use cases")
	}
	return &RuleActions{accounts: accounts, comments: comments}
}

var _ commentautomation.Actions = (*RuleActions)(nil)

func (a *RuleActions) manageable(ctx context.Context, rule *ca.Rule) (*ttdomain.Account, error) {
	account, err := a.accounts.FindByID(ctx, rule.AccountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != rule.WorkspaceID || !account.CanManageCommentsNow() {
		return nil, ErrAccountUnavailable
	}
	return account, nil
}

func (a *RuleActions) ReplyPublicly(ctx context.Context, rule *ca.Rule, commentID, text string) error {
	account, err := a.manageable(ctx, rule)
	if err != nil {
		return err
	}
	_, err = a.comments.reply(ttdomain.WithBackground(ctx), account, commentID, text)
	return err
}

func (a *RuleActions) ReplyPrivately(ctx context.Context, rule *ca.Rule, commentID, text string) error {
	return fmt.Errorf("%w: private replies on tiktok", ca.ErrActionUnsupported)
}

func (a *RuleActions) Hide(ctx context.Context, rule *ca.Rule, commentID string) error {
	account, err := a.manageable(ctx, rule)
	if err != nil {
		return err
	}
	_, err = a.comments.setHidden(ttdomain.WithBackground(ctx), account, commentID, true)
	return err
}

func (a *RuleActions) Delete(ctx context.Context, rule *ca.Rule, commentID string) error {
	return fmt.Errorf("%w: tiktok only deletes the account's own comments", ca.ErrActionUnsupported)
}

func (a *RuleActions) Like(ctx context.Context, rule *ca.Rule, commentID string) error {
	account, err := a.manageable(ctx, rule)
	if err != nil {
		return err
	}
	_, err = a.comments.setLiked(ttdomain.WithBackground(ctx), account, commentID, true)
	return err
}

type AccountOwnership struct {
	accounts ttdomain.AccountRepository
}

func NewAccountOwnership(accounts ttdomain.AccountRepository) *AccountOwnership {
	if accounts == nil {
		panic("tiktok: account ownership requires accounts")
	}
	return &AccountOwnership{accounts: accounts}
}

var _ commentautomation.AccountVerifier = (*AccountOwnership)(nil)

func (o *AccountOwnership) VerifyAccount(ctx context.Context, workspaceID, accountID string) error {
	_, err := o.accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	return err
}

func (o *AccountOwnership) AccountBelongsTo(ctx context.Context, workspaceID, accountID string) (bool, error) {
	err := o.VerifyAccount(ctx, workspaceID, accountID)
	if errors.Is(err, ttdomain.ErrAccountNotFound) {
		return false, nil
	}
	return err == nil, err
}
