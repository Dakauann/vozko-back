package comment_analysis

import (
	"fmt"
	"strings"
	"time"
)

// Answering a comment with the model's help (§6).
//
// The rule the plan puts in bold, and the reason this file is mostly a set of
// refusals: an automatic reply posts IN PUBLIC, in the customer's voice, to
// someone a machine classified. A wrong one is a screenshot. So the policy
// ships off, `auto` is per account and never a global default, and the gate is
// a closed allow-list rather than a blocklist: anything the engine could not
// confidently name is answered by a person or not at all.

// ReplyMode is what an account is allowed to do about a comment.
type ReplyMode string

const (
	// ReplyModeOff is the default and the shipped state: no drafting, no
	// posting.
	ReplyModeOff ReplyMode = "off"
	// ReplyModeSuggest drafts for a human, who edits and sends. This is the
	// mode the feature is actually meant to be used in.
	ReplyModeSuggest ReplyMode = "suggest"
	// ReplyModeAuto posts without a human, within the gate below.
	ReplyModeAuto ReplyMode = "auto"
)

func (m ReplyMode) Valid() bool {
	switch m {
	case ReplyModeOff, ReplyModeSuggest, ReplyModeAuto:
		return true
	}
	return false
}

const (
	// DefaultMaxAutoReplySeverity is the ceiling an account gets when it turns
	// auto on without choosing one. Well below HighSeverityThreshold on
	// purpose: the band between them is where a comment is bad enough to be
	// worth a careful human answer and not yet bad enough to be flagged.
	DefaultMaxAutoReplySeverity = 30
	// MaxReplyLength bounds a draft. Instagram's own limit is larger; this is
	// about what a person will actually read under a post.
	MaxReplyLength = 900
)

// autoReplyableIntents is the closed allow-list. A comment whose intent is not
// on it is never answered automatically, INCLUDING IntentOther: "we could not
// name what this person wants" is the worst possible basis for a public reply.
var autoReplyableIntents = map[Intent]bool{
	IntentQuestion:  true,
	IntentPraise:    true,
	IntentSalesLead: true,
}

// ReplyPolicy is one account's answer to "what may we say back".
type ReplyPolicy struct {
	Mode ReplyMode `json:"mode"`
	// MaxAutoSeverity is the ceiling for automatic replies. Above it a human
	// answers, whatever the intent says.
	MaxAutoSeverity int `json:"maxAutoSeverity"`
}

func DefaultReplyPolicy() ReplyPolicy {
	p := ReplyPolicy{Mode: ReplyModeOff}
	p.Normalize()
	return p
}

// Normalize fills the defaults and refuses to keep a value it does not
// recognise. An unknown mode becomes off rather than an error, because the
// safe reading of a corrupt setting is silence.
func (p *ReplyPolicy) Normalize() {
	p.Mode = ReplyMode(strings.ToLower(strings.TrimSpace(string(p.Mode))))
	if !p.Mode.Valid() {
		p.Mode = ReplyModeOff
	}
	if p.MaxAutoSeverity <= 0 {
		p.MaxAutoSeverity = DefaultMaxAutoReplySeverity
	}
	// The ceiling can never reach the point at which the engine itself calls a
	// comment severe. An operator who types 99 gets the highest value that
	// still means something, not the one they typed.
	if p.MaxAutoSeverity > HighSeverityThreshold {
		p.MaxAutoSeverity = HighSeverityThreshold
	}
}

func (p ReplyPolicy) Validate() error {
	if !p.Mode.Valid() {
		return fmt.Errorf("%w: reply mode %q", ErrInvalidFilter, p.Mode)
	}
	if p.MaxAutoSeverity < 0 || p.MaxAutoSeverity > HighSeverityThreshold {
		return fmt.Errorf("%w: auto-reply ceiling must be between 0 and %d", ErrInvalidFilter, HighSeverityThreshold)
	}
	return nil
}

// CanSuggest reports whether the account may have drafts written for it at
// all. `off` means off: no model call, no draft, nothing to accidentally send.
func (p ReplyPolicy) CanSuggest() bool {
	return p.Mode == ReplyModeSuggest || p.Mode == ReplyModeAuto
}

// CanAutoReply is the gate. Every clause is a reason NOT to post, and they are
// listed in the order they are cheapest to check.
func (p ReplyPolicy) CanAutoReply(c *CommentAnalysis) bool {
	if p.Mode != ReplyModeAuto || c == nil {
		return false
	}
	// An unclassified comment has no intent to gate on, so there is nothing to
	// be confident about.
	if c.Status != StatusAnalyzed {
		return false
	}
	// Already routed to a person. A machine answering first takes that choice
	// away from them.
	if c.RequiresAction {
		return false
	}
	// Replying to spam publicly amplifies it and confirms the account is live.
	if c.IsSpam {
		return false
	}
	// Never argue automatically. A critic may be answerable by a person; a
	// machine doing it in the customer's voice is the screenshot.
	if c.Stance == StanceHostile || c.Stance == StanceCritic {
		return false
	}
	if !autoReplyableIntents[c.Intent] {
		return false
	}
	return c.Severity <= p.MaxAutoSeverity
}

// ReplySuggestion is one drafted answer: the text, the comment it answers, and
// which model wrote it, so a customer asking "who wrote this" has an answer.
type ReplySuggestion struct {
	CommentID string    `json:"commentId"`
	Text      string    `json:"text"`
	Model     string    `json:"model,omitempty"`
	DraftedAt time.Time `json:"draftedAt"`
	// Auto marks a draft the pipeline produced and posted without a human, so
	// the feed can say which replies nobody read first.
	Auto bool `json:"auto"`
}

func NewReplySuggestion(commentID, text, model string) ReplySuggestion {
	text = strings.TrimSpace(text)
	if len(text) > MaxReplyLength {
		text = strings.TrimSpace(text[:MaxReplyLength])
	}
	return ReplySuggestion{
		CommentID: strings.TrimSpace(commentID),
		Text:      text,
		Model:     strings.TrimSpace(model),
		DraftedAt: time.Now().UTC(),
	}
}

func (s ReplySuggestion) Validate() error {
	if s.CommentID == "" {
		return fmt.Errorf("%w: the draft answers no comment", ErrInvalidFilter)
	}
	if s.Text == "" {
		return fmt.Errorf("%w: the draft is empty", ErrInvalidFilter)
	}
	return nil
}
