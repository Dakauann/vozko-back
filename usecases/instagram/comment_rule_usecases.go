package instagram

import (
	"context"
	"errors"
	"log"

	igdomain "vozko/domain/instagram"
)

type commentActions interface {
	ReplyPublicly(ctx context.Context, workspaceID, accountID, igCommentID, message string) (string, error)
	ReplyPrivately(ctx context.Context, workspaceID, accountID, igCommentID, text string) error
	SetHidden(ctx context.Context, workspaceID, accountID, igCommentID string, hidden bool) error
}

type EvaluateCommentRulesUseCase struct {
	rules   igdomain.CommentRuleRepository
	actions commentActions
}

func NewEvaluateCommentRulesUseCase(
	rules igdomain.CommentRuleRepository,
	actions commentActions,
) *EvaluateCommentRulesUseCase {
	return &EvaluateCommentRulesUseCase{rules: rules, actions: actions}
}

func (uc *EvaluateCommentRulesUseCase) Execute(ctx context.Context, comment *igdomain.Comment) {
	if uc == nil || uc.rules == nil || uc.actions == nil || comment == nil {
		return
	}
	if comment.IsOurs {
		return
	}

	candidates, err := uc.rules.ListCandidates(ctx, comment.IGAccountID, comment.IGMediaID)
	if err != nil {
		log.Printf("[instagram-rules] cannot load rules account=%s: %v", comment.IGAccountID, err)
		return
	}

	for _, rule := range candidates {
		if !rule.Matches(comment) {
			continue
		}
		log.Printf("[instagram-rules] rule %q (%s) matched comment=%s on media=%s",
			rule.Name, rule.ID, comment.IGCommentID, comment.IGMediaID)
		uc.apply(ctx, rule, comment)
		return
	}
}

func (uc *EvaluateCommentRulesUseCase) apply(ctx context.Context, rule *igdomain.CommentRule, comment *igdomain.Comment) {
	for _, action := range rule.Actions {
		switch action {
		case igdomain.ActionPublicReply:
			text := igdomain.RenderText(rule.PublicReplyText, comment)
			if text == "" {
				continue
			}
			if _, err := uc.actions.ReplyPublicly(ctx, rule.WorkspaceID, rule.IGAccountID, comment.IGCommentID, text); err != nil {
				log.Printf("[instagram-rules] public reply failed rule=%s comment=%s: %v", rule.ID, comment.IGCommentID, err)
			}

		case igdomain.ActionPrivateReply:
			text := igdomain.RenderText(rule.PrivateReplyText, comment)
			if text == "" {
				continue
			}
			if err := uc.actions.ReplyPrivately(ctx, rule.WorkspaceID, rule.IGAccountID, comment.IGCommentID, text); err != nil {
				if errors.Is(err, igdomain.ErrPrivateReplyUsed) || errors.Is(err, igdomain.ErrPrivateReplyExpired) {
					log.Printf("[instagram-rules] private reply unavailable for comment=%s: %v", comment.IGCommentID, err)
					continue
				}
				log.Printf("[instagram-rules] private reply failed rule=%s comment=%s: %v", rule.ID, comment.IGCommentID, err)
			}

		case igdomain.ActionHide:
			if err := uc.actions.SetHidden(ctx, rule.WorkspaceID, rule.IGAccountID, comment.IGCommentID, true); err != nil {
				log.Printf("[instagram-rules] hide failed rule=%s comment=%s: %v", rule.ID, comment.IGCommentID, err)
			}
		}
	}
}

type ManageCommentRulesUseCase struct {
	rules    igdomain.CommentRuleRepository
	accounts igdomain.AccountRepository
}

func NewManageCommentRulesUseCase(
	rules igdomain.CommentRuleRepository,
	accounts igdomain.AccountRepository,
) *ManageCommentRulesUseCase {
	return &ManageCommentRulesUseCase{rules: rules, accounts: accounts}
}

func (uc *ManageCommentRulesUseCase) List(ctx context.Context, workspaceID, accountID string) ([]*igdomain.CommentRule, error) {
	if _, err := uc.requireAccount(ctx, workspaceID, accountID); err != nil {
		return nil, err
	}
	return uc.rules.ListByAccount(ctx, workspaceID, accountID)
}

func (uc *ManageCommentRulesUseCase) Create(ctx context.Context, rule *igdomain.CommentRule) (*igdomain.CommentRule, error) {
	if _, err := uc.requireAccount(ctx, rule.WorkspaceID, rule.IGAccountID); err != nil {
		return nil, err
	}
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := uc.rules.Create(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (uc *ManageCommentRulesUseCase) Update(ctx context.Context, rule *igdomain.CommentRule) (*igdomain.CommentRule, error) {
	existing, err := uc.rules.FindByID(ctx, rule.WorkspaceID, rule.ID)
	if err != nil {
		return nil, err
	}
	rule.IGAccountID = existing.IGAccountID
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := uc.rules.Update(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (uc *ManageCommentRulesUseCase) Delete(ctx context.Context, workspaceID, id string) error {
	return uc.rules.Delete(ctx, workspaceID, id)
}

func (uc *ManageCommentRulesUseCase) requireAccount(ctx context.Context, workspaceID, accountID string) (*igdomain.Account, error) {
	account, err := uc.accounts.FindByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.WorkspaceID != workspaceID {
		return nil, igdomain.ErrAccountNotFound
	}
	return account, nil
}

type CommentActionRunner struct {
	reply    *ReplyToCommentUseCase
	private  *SendPrivateReplyUseCase
	moderate *ModerateCommentUseCase
}

func NewCommentActionRunner(
	reply *ReplyToCommentUseCase,
	private *SendPrivateReplyUseCase,
	moderate *ModerateCommentUseCase,
) *CommentActionRunner {
	return &CommentActionRunner{reply: reply, private: private, moderate: moderate}
}

func (r *CommentActionRunner) ReplyPublicly(ctx context.Context, workspaceID, accountID, igCommentID, message string) (string, error) {
	if r == nil || r.reply == nil {
		return "", nil
	}
	return r.reply.Execute(ctx, workspaceID, accountID, igCommentID, message)
}

func (r *CommentActionRunner) ReplyPrivately(ctx context.Context, workspaceID, accountID, igCommentID, text string) error {
	if r == nil || r.private == nil {
		return nil
	}
	return r.private.Execute(ctx, workspaceID, accountID, igCommentID, text)
}

func (r *CommentActionRunner) SetHidden(ctx context.Context, workspaceID, accountID, igCommentID string, hidden bool) error {
	if r == nil || r.moderate == nil {
		return nil
	}
	return r.moderate.SetHidden(ctx, workspaceID, accountID, igCommentID, hidden)
}
