package instagram

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Comment automation: a stored rule reacts to a public comment.
//
// The rule is DATA — an ordered list of conditions and actions — rather than a
// hardcoded if-chain, so a later "comment received" workflow trigger can consume
// the same comment events without either engine rewriting the other.
//
// It deliberately stops at the comment. When a rule sends a private reply it
// opens a DM conversation, and from that point the existing agent/workflow
// automation attends it: the branching a marketer wants after "hello" happens on
// the conversation, where the whole workflow canvas already works.

var (
	ErrCommentRuleNotFound      = errors.New("instagram: comment rule not found")
	ErrCommentRuleNoActions     = errors.New("instagram: comment rule must define at least one action")
	ErrCommentRuleNameRequired  = errors.New("instagram: comment rule name is required")
	ErrCommentRuleEmptyKeywords = errors.New("instagram: keyword match requires at least one keyword")
	ErrCommentRuleReplyEmpty    = errors.New("instagram: reply action requires a message")
)

// CommentRuleMatch is how a rule's keywords are compared against comment text.
type CommentRuleMatch string

const (
	// MatchAny fires on every comment. Used for blanket moderation or a catch-all
	// auto-reply; keywords are ignored.
	MatchAny CommentRuleMatch = "any"
	// MatchContains fires when the comment contains any keyword.
	MatchContains CommentRuleMatch = "contains"
	// MatchExact fires when the whole comment equals a keyword.
	MatchExact CommentRuleMatch = "exact"
)

// CommentRuleAction is one thing a rule does when it matches.
type CommentRuleAction string

const (
	// ActionPublicReply posts a threaded reply under the comment.
	ActionPublicReply CommentRuleAction = "public_reply"
	// ActionPrivateReply DMs the comment's author, which opens a conversation the
	// agent or workflow then attends. Instagram allows exactly one per comment,
	// ever, within 7 days.
	ActionPrivateReply CommentRuleAction = "private_reply"
	// ActionHide hides the comment. Hiding works on anyone's comment because we
	// hold the media-owner token; deletion does not, so it is not offered.
	ActionHide CommentRuleAction = "hide"
)

// CommentRule reacts to comments on one account.
type CommentRule struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	IGAccountID string `json:"igAccountId"`

	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	// IGMediaID scopes the rule to a single post. Empty means the rule applies to
	// every post on the account — the "account default" tier.
	IGMediaID string `json:"igMediaId,omitempty"`

	Match    CommentRuleMatch `json:"match"`
	Keywords []string         `json:"keywords"`

	// Actions run in this order. Ordering is meaningful: hiding a comment before
	// replying to it would reply to something no one can see.
	Actions []CommentRuleAction `json:"actions"`

	// PublicReplyText and PrivateReplyText support {{username}} and {{comment}}.
	PublicReplyText  string `json:"publicReplyText,omitempty"`
	PrivateReplyText string `json:"privateReplyText,omitempty"`

	// Priority orders rules against each other; lower runs first. The first
	// matching rule wins, so a specific post rule can pre-empt an account default.
	Priority int `json:"priority"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Normalize trims and canonicalizes a rule before validation or storage.
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
		key := foldForMatch(kw)
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

// Validate rejects a rule that could not act.
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

// Matches reports whether this rule should fire for a comment.
//
// A rule never reacts to our own comment: our replies arrive back as webhooks,
// so without this a reply rule would answer itself forever.
func (r *CommentRule) Matches(c *Comment) bool {
	if r == nil || c == nil || !r.Enabled {
		return false
	}
	if c.IsOurs {
		return false
	}
	if c.Hidden {
		// Already moderated; re-acting would fight an operator's decision.
		return false
	}
	if r.IGMediaID != "" && r.IGMediaID != c.IGMediaID {
		return false
	}
	if r.Match == MatchAny {
		return true
	}

	text := foldForMatch(c.Text)
	if text == "" {
		return false
	}
	for _, kw := range r.Keywords {
		folded := foldForMatch(kw)
		if folded == "" {
			continue
		}
		switch r.Match {
		case MatchExact:
			if text == folded {
				return true
			}
		default: // MatchContains
			if strings.Contains(text, folded) {
				return true
			}
		}
	}
	return false
}

// RenderText fills a rule's message template.
//
// Kept to two variables on purpose: a public comment reply is a permanent,
// brand-facing artifact, so the surface a marketer can get wrong is small.
func RenderText(template string, c *Comment) string {
	if c == nil {
		return template
	}
	username := c.FromUsername
	out := strings.ReplaceAll(template, "{{username}}", username)
	out = strings.ReplaceAll(out, "{{comment}}", c.Text)
	return strings.TrimSpace(out)
}

// accentFolds maps the accented letters that appear in pt-BR and es to their
// bare form. Kept as an explicit table rather than pulling in a Unicode
// normalization dependency: the alphabet a commenter can type here is small and
// known, and the table is trivial to extend.
var accentFolds = map[rune]rune{
	'á': 'a', 'à': 'a', 'ã': 'a', 'â': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'õ': 'o', 'ô': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// foldForMatch makes keyword matching forgiving in the way users expect:
// case- and accent-insensitive, so "promoção", "PROMOCAO" and "Promocao" are one
// keyword. A Brazilian audience types all three, and a rule that only matched
// the accented spelling would silently miss most comments.
func foldForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if folded, ok := accentFolds[r]; ok {
			b.WriteRune(folded)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CommentRuleRepository stores comment rules.
type CommentRuleRepository interface {
	Create(ctx context.Context, rule *CommentRule) error
	Update(ctx context.Context, rule *CommentRule) error
	Delete(ctx context.Context, workspaceID, id string) error
	FindByID(ctx context.Context, workspaceID, id string) (*CommentRule, error)
	// ListByAccount returns every rule for an account, ordered by priority.
	ListByAccount(ctx context.Context, workspaceID, igAccountID string) ([]*CommentRule, error)
	// ListCandidates returns the enabled rules that could match a post: those
	// scoped to it, plus the account-wide defaults, ordered by priority.
	ListCandidates(ctx context.Context, igAccountID, igMediaID string) ([]*CommentRule, error)
}
