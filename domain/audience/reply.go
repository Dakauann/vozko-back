package audience

import (
	"fmt"
	"strings"
	"time"
)

type ReplyMode string

const (
	ReplyModeOff     ReplyMode = "off"
	ReplyModeSuggest ReplyMode = "suggest"
	ReplyModeAuto    ReplyMode = "auto"
)

func (m ReplyMode) Valid() bool {
	switch m {
	case ReplyModeOff, ReplyModeSuggest, ReplyModeAuto:
		return true
	}
	return false
}

const (
	DefaultMaxAutoReplySeverity = 30
	MaxReplyLength              = 900
)

var autoReplyableIntents = map[Intent]bool{
	IntentQuestion:  true,
	IntentPraise:    true,
	IntentSalesLead: true,
}

type ReplyPolicy struct {
	Mode            ReplyMode `json:"mode"`
	MaxAutoSeverity int       `json:"maxAutoSeverity"`
}

func DefaultReplyPolicy() ReplyPolicy {
	p := ReplyPolicy{Mode: ReplyModeOff}
	p.Normalize()
	return p
}

func (p *ReplyPolicy) Normalize() {
	p.Mode = ReplyMode(strings.ToLower(strings.TrimSpace(string(p.Mode))))
	if !p.Mode.Valid() {
		p.Mode = ReplyModeOff
	}
	if p.MaxAutoSeverity <= 0 {
		p.MaxAutoSeverity = DefaultMaxAutoReplySeverity
	}
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

func (p ReplyPolicy) CanSuggest() bool {
	return p.Mode == ReplyModeSuggest || p.Mode == ReplyModeAuto
}

func (p ReplyPolicy) CanAutoReply(c *Analysis) bool {
	if p.Mode != ReplyModeAuto || c == nil {
		return false
	}
	if c.Status != StatusAnalyzed {
		return false
	}
	if c.RequiresAction {
		return false
	}
	if c.IsSpam {
		return false
	}
	if c.Stance == StanceHostile || c.Stance == StanceCritic {
		return false
	}
	if !autoReplyableIntents[c.Intent] {
		return false
	}
	return c.Severity <= p.MaxAutoSeverity
}

type ReplySuggestion struct {
	CommentID string    `json:"commentId"`
	Text      string    `json:"text"`
	Model     string    `json:"model,omitempty"`
	DraftedAt time.Time `json:"draftedAt"`
	Auto      bool      `json:"auto"`
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
