package commentautomation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

var (
	ErrRuleNotFound      = errors.New("comment rule not found")
	ErrNoActions         = errors.New("comment rule must define at least one action")
	ErrNameRequired      = errors.New("comment rule name is required")
	ErrEmptyKeywords     = errors.New("keyword match requires at least one keyword")
	ErrReplyEmpty        = errors.New("reply action requires a message")
	ErrUnknownAction     = errors.New("unknown comment rule action")
	ErrActionUnsupported = errors.New("this channel does not support the comment rule action")
	ErrUnknownSource     = errors.New("comment rules are not available on this channel")
)

type Match string

const (
	MatchAny      Match = "any"
	MatchContains Match = "contains"
	MatchExact    Match = "exact"
)

type Action string

const (
	ActionPublicReply  Action = "public_reply"
	ActionPrivateReply Action = "private_reply"
	ActionHide         Action = "hide"
	ActionDelete       Action = "delete"
	ActionLike         Action = "like"
)

var knownActions = map[Action]struct{}{
	ActionPublicReply: {}, ActionPrivateReply: {}, ActionHide: {}, ActionDelete: {}, ActionLike: {},
}

var sourceActions = map[shared.EntryType]map[Action]struct{}{
	shared.EntryTypeInstagram: {ActionPublicReply: {}, ActionPrivateReply: {}, ActionHide: {}},
	shared.EntryTypeFacebook:  {ActionPublicReply: {}, ActionPrivateReply: {}, ActionHide: {}, ActionDelete: {}, ActionLike: {}},
}

func Sources() []shared.EntryType {
	return []shared.EntryType{shared.EntryTypeInstagram, shared.EntryTypeFacebook}
}

type Rule struct {
	ID          string
	WorkspaceID string
	Source      shared.EntryType
	AccountID   string
	ContainerID string

	Name    string
	Enabled bool

	Match    Match
	Keywords []string
	Actions  []Action

	PublicReplyText  string
	PrivateReplyText string

	Priority int

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Subject struct {
	ContainerID string
	AuthorName  string
	Text        string
	IsOurs      bool
	Hidden      bool
}

func (r *Rule) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.ContainerID = strings.TrimSpace(r.ContainerID)
	r.PublicReplyText = strings.TrimSpace(r.PublicReplyText)
	r.PrivateReplyText = strings.TrimSpace(r.PrivateReplyText)
	if r.Match == "" {
		r.Match = MatchContains
	}

	keywords := make([]string, 0, len(r.Keywords))
	seen := make(map[string]struct{}, len(r.Keywords))
	for _, kw := range r.Keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		key := shared.FoldForMatch(kw)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keywords = append(keywords, kw)
	}
	r.Keywords = keywords

	actions := make([]Action, 0, len(r.Actions))
	seenAction := make(map[Action]struct{}, len(r.Actions))
	for _, a := range r.Actions {
		if _, dup := seenAction[a]; dup {
			continue
		}
		seenAction[a] = struct{}{}
		actions = append(actions, a)
	}
	r.Actions = actions
}

func (r *Rule) Validate() error {
	supported, ok := sourceActions[r.Source]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownSource, r.Source)
	}
	if r.Name == "" {
		return ErrNameRequired
	}
	if len(r.Actions) == 0 {
		return ErrNoActions
	}
	if r.Match != MatchAny && len(r.Keywords) == 0 {
		return ErrEmptyKeywords
	}
	for _, a := range r.Actions {
		if _, known := knownActions[a]; !known {
			return fmt.Errorf("%w: %s", ErrUnknownAction, a)
		}
		if _, allowed := supported[a]; !allowed {
			return fmt.Errorf("%w: %s on %s", ErrActionUnsupported, a, r.Source)
		}
		if a == ActionPublicReply && r.PublicReplyText == "" || a == ActionPrivateReply && r.PrivateReplyText == "" {
			return ErrReplyEmpty
		}
	}
	return nil
}

func (r *Rule) Matches(s Subject) bool {
	if r == nil || !r.Enabled || s.IsOurs || s.Hidden {
		return false
	}
	if r.ContainerID != "" && r.ContainerID != s.ContainerID {
		return false
	}
	if r.Match == MatchAny {
		return true
	}
	text := shared.FoldForMatch(s.Text)
	if text == "" {
		return false
	}
	for _, kw := range r.Keywords {
		folded := shared.FoldForMatch(kw)
		if folded == "" {
			continue
		}
		if r.Match == MatchExact && text == folded || r.Match != MatchExact && strings.Contains(text, folded) {
			return true
		}
	}
	return false
}

func RenderText(template string, s Subject) string {
	out := strings.ReplaceAll(template, "{{username}}", s.AuthorName)
	out = strings.ReplaceAll(out, "{{comment}}", s.Text)
	return strings.TrimSpace(out)
}

type Repository interface {
	Create(ctx context.Context, rule *Rule) error
	Update(ctx context.Context, rule *Rule) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*Rule, error)
	ListByAccount(ctx context.Context, workspaceID string, source shared.EntryType, accountID string) ([]*Rule, error)
	ListCandidates(ctx context.Context, source shared.EntryType, accountID, containerID string) ([]*Rule, error)
}
