package instagram

import (
	"context"
	"errors"
	"log"

	igdomain "vozko/domain/instagram"
)

// Comment automation. A stored rule reacts to an inbound public comment.
//
// The evaluator stops at the comment: its most valuable action, the private
// reply, opens a DM conversation, and from there the existing agent/workflow
// automation attends it. So this file contains no AI and no branching — the
// branching a marketer wants after "hello" already lives on the conversation.

// commentActions is the set of provider calls a rule can make, kept as a narrow
// port so the evaluator can be tested without Meta.
type commentActions interface {
	ReplyPublicly(ctx context.Context, workspaceID, accountID, igCommentID, message string) (string, error)
	ReplyPrivately(ctx context.Context, workspaceID, accountID, igCommentID, text string) error
	SetHidden(ctx context.Context, workspaceID, accountID, igCommentID string, hidden bool) error
}

// EvaluateCommentRulesUseCase applies an account's rules to one comment.
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

// Execute runs the first matching rule for a comment.
//
// FIRST match wins, not every match: a comment that trips both a promo rule and
// a spam rule must not be replied to and hidden at once. Rules are ordered by
// priority, with post-scoped rules ahead of account defaults, so the specific
// intent wins.
//
// Errors are logged rather than returned: a failed automation must not fail the
// webhook, because the webhook's redelivery would re-run the rule and could send
// a second public reply.
func (uc *EvaluateCommentRulesUseCase) Execute(ctx context.Context, comment *igdomain.Comment) {
	if uc == nil || uc.rules == nil || uc.actions == nil || comment == nil {
		return
	}
	// Our own replies come back as webhooks. Without this a public-reply rule
	// would answer its own answer, forever.
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

// apply runs a matched rule's actions in their declared order.
//
// Order matters and is preserved from the rule: replying to a comment after
// hiding it would answer something nobody can see. One action failing does not
// abort the rest — hiding spam is still worth doing if the reply failed.
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
			// Instagram allows exactly one private reply per comment, ever. The
			// allowance is claimed in Postgres before the send, so a rule that
			// somehow runs twice cannot burn it twice — the second attempt is
			// rejected here rather than at Meta.
			if err := uc.actions.ReplyPrivately(ctx, rule.WorkspaceID, rule.IGAccountID, comment.IGCommentID, text); err != nil {
				// Both are expected outcomes, not faults: the allowance is
				// one-per-comment and expires after 7 days.
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

// ---------------------------------------------------------------- rule CRUD

// ManageCommentRulesUseCase is the workspace-facing CRUD for rules.
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

// List returns every rule on an account, ordered by priority.
func (uc *ManageCommentRulesUseCase) List(ctx context.Context, workspaceID, accountID string) ([]*igdomain.CommentRule, error) {
	if _, err := uc.requireAccount(ctx, workspaceID, accountID); err != nil {
		return nil, err
	}
	return uc.rules.ListByAccount(ctx, workspaceID, accountID)
}

// Create stores a new rule.
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

// Update replaces a rule's configuration.
func (uc *ManageCommentRulesUseCase) Update(ctx context.Context, rule *igdomain.CommentRule) (*igdomain.CommentRule, error) {
	existing, err := uc.rules.FindByID(ctx, rule.WorkspaceID, rule.ID)
	if err != nil {
		return nil, err
	}
	// The owning account is immutable: moving a rule between accounts would
	// silently change which audience it answers.
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

// requireAccount confirms the account belongs to the workspace, so a rule can
// never be attached to another tenant's account.
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

// CommentActionRunner adapts the three existing comment usecases onto the
// evaluator's port, so automation performs exactly the same operations — with
// the same provider rules, mirror updates and one-shot claims — as an operator
// clicking the equivalent button.
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
