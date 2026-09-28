package commentautomation

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	rules ca.Repository
}

func NewEvaluator(rules ca.Repository) *Evaluator {
	return &Evaluator{rules: rules}
}

func (e *Evaluator) Evaluate(ctx context.Context, source shared.EntryType, accountID, commentID string, subject ca.Subject, actions Actions) {
	if subject.IsOurs {
		return
	}
	candidates, err := e.rules.ListCandidates(ctx, source, accountID, subject.ContainerID)
	if err != nil {
		log.Printf("[comment-rules] %s account %s: rules unavailable: %v", source, accountID, err)
		return
	}
	for _, rule := range candidates {
		if !rule.Matches(subject) {
			continue
		}
		log.Printf("[comment-rules] %s rule %q (%s) matched comment %s", source, rule.Name, rule.ID, commentID)
		apply(ctx, rule, commentID, subject, actions)
		return
	}
}

func apply(ctx context.Context, rule *ca.Rule, commentID string, subject ca.Subject, actions Actions) {
	for _, action := range rule.Actions {
		var err error
		switch action {
		case ca.ActionPublicReply:
			if text := ca.RenderText(rule.PublicReplyText, subject); text != "" {
				err = actions.ReplyPublicly(ctx, rule, commentID, text)
			}
		case ca.ActionPrivateReply:
			if text := ca.RenderText(rule.PrivateReplyText, subject); text != "" {
				err = actions.ReplyPrivately(ctx, rule, commentID, text)
			}
		case ca.ActionHide:
			err = actions.Hide(ctx, rule, commentID)
		case ca.ActionDelete:
			err = actions.Delete(ctx, rule, commentID)
		case ca.ActionLike:
			err = actions.Like(ctx, rule, commentID)
		}
		if err != nil {
			log.Printf("[comment-rules] %s rule %s action %s on comment %s: %v", rule.Source, rule.ID, action, commentID, err)
		}
	}
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
