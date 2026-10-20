package commentautomation

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
	"vozko/domain/shared"
)

type Actions interface {
	ReplyPublicly(ctx context.Context, rule *ca.Rule, commentID, text string) error
	ReplyPrivately(ctx context.Context, rule *ca.Rule, commentID, text string) error
	Hide(ctx context.Context, rule *ca.Rule, commentID string) error
	Delete(ctx context.Context, rule *ca.Rule, commentID string) error
	Like(ctx context.Context, rule *ca.Rule, commentID string) error
}

type Evaluator struct {
	rules  ca.Repository
	claims ca.ExecutionClaims
}

func NewEvaluator(rules ca.Repository, claims ca.ExecutionClaims) *Evaluator {
	if rules == nil || claims == nil {
		panic("comment rules evaluator: rules and execution claims are required")
	}
	return &Evaluator{rules: rules, claims: claims}
}

func (e *Evaluator) Evaluate(ctx context.Context, source shared.EntryType, accountID, commentID string, subject ca.Subject, actions Actions) error {
	if subject.IsOurs {
		return nil
	}
	if strings.TrimSpace(commentID) == "" {
		return fmt.Errorf("%w: %s account %s", ca.ErrCommentIDRequired, source, accountID)
	}
	candidates, err := e.rules.ListCandidates(ctx, source, accountID, subject.ContainerID)
	if err != nil {
		return fmt.Errorf("%w: %s account %s: %w", ca.ErrRulesUnavailable, source, accountID, err)
	}
	for _, rule := range candidates {
		if !rule.Matches(subject) {
			continue
		}
		claimed, err := e.claims.Claim(ctx, source, commentID, rule.ID)
		if err != nil {
			return fmt.Errorf("%w: %s comment %s: %w", ca.ErrExecutionUnclaimed, source, commentID, err)
		}
		if !claimed {
			log.Printf("[comment-rules] %s comment %s already ran its rule; skipping", source, commentID)
			return nil
		}
		log.Printf("[comment-rules] %s rule %q (%s) matched comment %s", source, rule.Name, rule.ID, commentID)
		return apply(ctx, rule, commentID, subject, actions)
	}
	return nil
}

func apply(ctx context.Context, rule *ca.Rule, commentID string, subject ca.Subject, actions Actions) error {
	var failures []error
	for _, action := range rule.Actions {
		if err := run(ctx, rule, action, commentID, subject, actions); err != nil {
			failures = append(failures, fmt.Errorf("%w: %s rule %s action %s on comment %s: %w", ca.ErrActionFailed, rule.Source, rule.ID, action, commentID, err))
		}
	}
	return errors.Join(failures...)
}

func run(ctx context.Context, rule *ca.Rule, action ca.Action, commentID string, subject ca.Subject, actions Actions) error {
	switch action {
	case ca.ActionPublicReply:
		return reply(rule, action, ca.RenderText(rule.PublicReplyText, subject), func(text string) error {
			return actions.ReplyPublicly(ctx, rule, commentID, text)
		})
	case ca.ActionPrivateReply:
		return reply(rule, action, ca.RenderText(rule.PrivateReplyText, subject), func(text string) error {
			return actions.ReplyPrivately(ctx, rule, commentID, text)
		})
	case ca.ActionHide:
		return actions.Hide(ctx, rule, commentID)
	case ca.ActionDelete:
		return actions.Delete(ctx, rule, commentID)
	case ca.ActionLike:
		return actions.Like(ctx, rule, commentID)
	}
	return fmt.Errorf("%w: %s", ca.ErrUnknownAction, action)
}

func reply(rule *ca.Rule, action ca.Action, text string, send func(string) error) error {
	if text == "" {
		return nil
	}
	if err := ca.CheckReply(rule.Source, action, text); err != nil {
		return err
	}
	return send(text)
}

type ExecutionPurge struct {
	claims ca.ExecutionClaims
	now    func() time.Time
}

func NewExecutionPurge(claims ca.ExecutionClaims, now func() time.Time) *ExecutionPurge {
	if claims == nil || now == nil {
		panic("comment rule execution purge: claims and clock are required")
	}
	return &ExecutionPurge{claims: claims, now: now}
}

func (p *ExecutionPurge) Execute(ctx context.Context) error {
	purged, err := p.claims.PurgeOlderThan(ctx, p.now().Add(-ca.ExecutionRetention))
	if err != nil {
		return err
	}
	if purged > 0 {
		log.Printf("[comment-rules] purged %d execution claim(s) older than %s", purged, ca.ExecutionRetention)
	}
	return nil
}

type AccountVerifier interface {
	VerifyAccount(ctx context.Context, workspaceID, accountID string) error
}

type Manager struct {
	rules    ca.Repository
	accounts map[shared.EntryType]AccountVerifier
}

func NewManager(rules ca.Repository, accounts map[shared.EntryType]AccountVerifier) *Manager {
	if accounts == nil {
		accounts = map[shared.EntryType]AccountVerifier{}
	}
	return &Manager{rules: rules, accounts: accounts}
}

func (m *Manager) Register(source shared.EntryType, verifier AccountVerifier) {
	m.accounts[source] = verifier
}

func (m *Manager) verify(ctx context.Context, workspaceID string, source shared.EntryType, accountID string) error {
	verifier, ok := m.accounts[source]
	if !ok {
		return fmt.Errorf("%w: %q", ca.ErrUnknownSource, source)
	}
	return verifier.VerifyAccount(ctx, workspaceID, accountID)
}

func (m *Manager) List(ctx context.Context, workspaceID string, source shared.EntryType, accountID string) ([]*ca.Rule, error) {
	if err := m.verify(ctx, workspaceID, source, accountID); err != nil {
		return nil, err
	}
	return m.rules.ListByAccount(ctx, workspaceID, source, accountID)
}

func (m *Manager) Create(ctx context.Context, rule *ca.Rule) (*ca.Rule, error) {
	if err := m.verify(ctx, rule.WorkspaceID, rule.Source, rule.AccountID); err != nil {
		return nil, err
	}
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := m.rules.Create(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (m *Manager) Update(ctx context.Context, rule *ca.Rule) (*ca.Rule, error) {
	if _, err := m.owned(ctx, rule.WorkspaceID, rule.Source, rule.AccountID, rule.ID); err != nil {
		return nil, err
	}
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		return nil, err
	}
	if err := m.rules.Update(ctx, rule); err != nil {
		return nil, err
	}
	return rule, nil
}

func (m *Manager) Delete(ctx context.Context, workspaceID string, source shared.EntryType, accountID, id string) error {
	if _, err := m.owned(ctx, workspaceID, source, accountID, id); err != nil {
		return err
	}
	return m.rules.Delete(ctx, workspaceID, id)
}

func (m *Manager) owned(ctx context.Context, workspaceID string, source shared.EntryType, accountID, id string) (*ca.Rule, error) {
	if err := m.verify(ctx, workspaceID, source, accountID); err != nil {
		return nil, err
	}
	existing, err := m.rules.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if existing.Source != source || existing.AccountID != accountID {
		return nil, ca.ErrRuleNotFound
	}
	return existing, nil
}

type Delivery struct {
	RecipientRef string
	MessageID    string
}

type DeliverFunc func(ctx context.Context) (*Delivery, error)

type PrivateReplySender struct {
	records privatereply.Repository
	now     func() time.Time
}

func NewPrivateReplySender(records privatereply.Repository) *PrivateReplySender {
	return &PrivateReplySender{records: records, now: func() time.Time { return time.Now().UTC() }}
}

type codedError interface {
	error
	ErrorCode() (code, subcode int)
}

func (s *PrivateReplySender) Send(ctx context.Context, source shared.EntryType, accountID, commentID string, commentedAt *time.Time, deliver DeliverFunc) (*Delivery, error) {
	if err := privatereply.CheckDeadline(commentedAt, s.now()); err != nil {
		return nil, err
	}
	claimed, err := s.records.Claim(ctx, source, commentID, accountID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, privatereply.ErrUsed
	}

	out, err := deliver(ctx)
	if err != nil {
		code := 0
		var coded codedError
		if errors.As(err, &coded) {
			code, _ = coded.ErrorCode()
		}
		if markErr := s.records.MarkFailed(ctx, source, commentID, code, err.Error()); markErr != nil {
			log.Printf("[private-reply] %s comment %s failure not recorded: %v", source, commentID, markErr)
		}
		return nil, err
	}
	if markErr := s.records.MarkSent(ctx, source, commentID, out.RecipientRef, out.MessageID); markErr != nil {
		log.Printf("[private-reply] %s comment %s delivery not recorded: %v", source, commentID, markErr)
	}
	return out, nil
}
