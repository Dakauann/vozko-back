package instagram

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
)

var (
	ErrCommentRuleNotFound      = errors.New("instagram: comment rule not found")
	ErrCommentRuleNoActions     = errors.New("instagram: comment rule must define at least one action")
	ErrCommentRuleNameRequired  = errors.New("instagram: comment rule name is required")
	ErrCommentRuleEmptyKeywords = errors.New("instagram: keyword match requires at least one keyword")
	ErrCommentRuleReplyEmpty    = errors.New("instagram: reply action requires a message")
)

type CommentRuleMatch string

const (
	MatchAny      CommentRuleMatch = "any"
	MatchContains CommentRuleMatch = "contains"
	MatchExact    CommentRuleMatch = "exact"
)

type CommentRuleAction string

const (
	ActionPublicReply  CommentRuleAction = "public_reply"
	ActionPrivateReply CommentRuleAction = "private_reply"
	ActionHide         CommentRuleAction = "hide"
)

type CommentRule struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`

	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	IGMediaID string `json:"igMediaId,omitempty"`

	Match    CommentRuleMatch `json:"match"`
	Keywords []string         `json:"keywords"`

	Actions []CommentRuleAction `json:"actions"`

	PublicReplyText  string `json:"publicReplyText,omitempty"`
	PrivateReplyText string `json:"privateReplyText,omitempty"`

	Priority int `json:"priority"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (r *CommentRule) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.IGMediaID = strings.TrimSpace(r.IGMediaID)
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

	actions := make([]CommentRuleAction, 0, len(r.Actions))
	seenAction := make(map[CommentRuleAction]struct{}, len(r.Actions))
	for _, a := range r.Actions {
		if _, dup := seenAction[a]; dup {
			continue
		}
		seenAction[a] = struct{}{}
		actions = append(actions, a)
	}
	r.Actions = actions
}

func (r *CommentRule) Validate() error {
	if r.Name == "" {
		return ErrCommentRuleNameRequired
	}
	if len(r.Actions) == 0 {
		return ErrCommentRuleNoActions
	}
	if r.Match != MatchAny && len(r.Keywords) == 0 {
		return ErrCommentRuleEmptyKeywords
	}
	for _, a := range r.Actions {
		switch a {
		case ActionPublicReply:
			if r.PublicReplyText == "" {
				return ErrCommentRuleReplyEmpty
			}
		case ActionPrivateReply:
			if r.PrivateReplyText == "" {
				return ErrCommentRuleReplyEmpty
			}
		case ActionHide:
		default:
			return errors.New("instagram: unknown comment rule action " + string(a))
		}
	}
	return nil
}

func (r *CommentRule) Matches(c *Comment) bool {
	if r == nil || c == nil || !r.Enabled {
		return false
	}
	if c.IsOurs {
		return false
	}
	if c.Hidden {
		return false
	}
	if r.IGMediaID != "" && r.IGMediaID != c.IGMediaID {
		return false
	}
	if r.Match == MatchAny {
		return true
	}

	text := shared.FoldForMatch(c.Text)
	if text == "" {
		return false
	}
	for _, kw := range r.Keywords {
		folded := shared.FoldForMatch(kw)
		if folded == "" {
			continue
		}
		switch r.Match {
		case MatchExact:
			if text == folded {
				return true
			}
		default:
			if strings.Contains(text, folded) {
				return true
			}
		}
	}
	return false
}

func RenderText(template string, c *Comment) string {
	if c == nil {
		return template
	}
	username := c.FromUsername
	out := strings.ReplaceAll(template, "{{username}}", username)
	out = strings.ReplaceAll(out, "{{comment}}", c.Text)
	return strings.TrimSpace(out)
}

type CommentRuleRepository interface {
	Create(ctx context.Context, rule *CommentRule) error
	Update(ctx context.Context, rule *CommentRule) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*CommentRule, error)
	ListByAccount(ctx context.Context, workspaceID, igAccountID string) ([]*CommentRule, error)
	ListCandidates(ctx context.Context, igAccountID, igMediaID string) ([]*CommentRule, error)
}
