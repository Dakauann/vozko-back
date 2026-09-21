package audience

import (
	"strings"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestReplyPolicyShipsOff(t *testing.T) {
	s := NewSettings("ws-1", SourceInstagram, "acc-1", VerticalServices)
	if s.ReplyPolicy.Mode != ReplyModeOff {
		t.Fatalf("mode = %q, a fresh account must not reply", s.ReplyPolicy.Mode)
	}
	if s.ReplyPolicy.CanSuggest() {
		t.Fatal("off means off: no drafting either")
	}
	if s.ReplyPolicy.CanAutoReply(analysedComment(IntentQuestion, 10)) {
		t.Fatal("a fresh account must never auto-reply")
	}
}

func analysedComment(intent Intent, severity int) *Analysis {
	return &Analysis{
		Status:     StatusAnalyzed,
		Intent:     intent,
		Stance:     StanceNeutral,
		Sentiment:  shared.SentimentNeutral,
		Severity:   severity,
		Excerpt:    "onde eu compro?",
		OccurredAt: time.Now().UTC(),
	}
}

func TestReplyPolicySuggestDraftsButNeverPosts(t *testing.T) {
	p := ReplyPolicy{Mode: ReplyModeSuggest}
	p.Normalize()
	if !p.CanSuggest() {
		t.Fatal("suggest must draft")
	}
	if p.CanAutoReply(analysedComment(IntentQuestion, 5)) {
		t.Fatal("suggest must never post on its own")
	}
}

func TestReplyPolicyAutoGate(t *testing.T) {
	p := ReplyPolicy{Mode: ReplyModeAuto}
	p.Normalize()

	cases := map[string]struct {
		comment *Analysis
		want    bool
	}{
		"a question below the ceiling": {comment: analysedComment(IntentQuestion, 10), want: true},
		"praise":                       {comment: analysedComment(IntentPraise, 0), want: true},
		"a buying signal":              {comment: analysedComment(IntentSalesLead, 5), want: true},
		"a complaint":                  {comment: analysedComment(IntentComplaint, 5), want: false},
		"a support request":            {comment: analysedComment(IntentSupportRequest, 5), want: false},
		"something we could not name":  {comment: analysedComment(IntentOther, 0), want: false},
		"a question that is severe":    {comment: analysedComment(IntentQuestion, 90), want: false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := p.CanAutoReply(c.comment); got != c.want {
				t.Fatalf("CanAutoReply = %v, want %v", got, c.want)
			}
		})
	}
}

func TestReplyPolicyAutoRefusesOnOverridingSignals(t *testing.T) {
	p := ReplyPolicy{Mode: ReplyModeAuto}
	p.Normalize()

	t.Run("a comment flagged for a human", func(t *testing.T) {
		c := analysedComment(IntentQuestion, 10)
		c.RequiresAction = true
		if p.CanAutoReply(c) {
			t.Fatal("a comment already routed to a person must not be answered by a machine")
		}
	})
	t.Run("spam", func(t *testing.T) {
		c := analysedComment(IntentQuestion, 0)
		c.IsSpam = true
		if p.CanAutoReply(c) {
			t.Fatal("replying to spam publicly amplifies it")
		}
	})
	t.Run("a hostile author", func(t *testing.T) {
		c := analysedComment(IntentQuestion, 10)
		c.Stance = StanceHostile
		if p.CanAutoReply(c) {
			t.Fatal("never argue with a hostile comment automatically")
		}
	})
	t.Run("a comment that was never classified", func(t *testing.T) {
		c := analysedComment(IntentQuestion, 10)
		c.Status = StatusPending
		if p.CanAutoReply(c) {
			t.Fatal("an unclassified comment has no intent to gate on")
		}
	})
	t.Run("nil", func(t *testing.T) {
		if p.CanAutoReply(nil) {
			t.Fatal("nil must not pass the gate")
		}
	})
}

func TestReplyPolicyNormalize(t *testing.T) {
	p := ReplyPolicy{Mode: "AUTO"}
	p.Normalize()
	if p.Mode != ReplyModeAuto {
		t.Fatalf("mode = %q, a stored value's case must not change its meaning", p.Mode)
	}
	if p.MaxAutoSeverity != DefaultMaxAutoReplySeverity {
		t.Fatalf("ceiling = %d, want the default", p.MaxAutoSeverity)
	}

	unknown := ReplyPolicy{Mode: "yolo"}
	unknown.Normalize()
	if unknown.Mode != ReplyModeOff {
		t.Fatalf("an unrecognised mode must fall back to off, got %q", unknown.Mode)
	}

	tooHigh := ReplyPolicy{Mode: ReplyModeAuto, MaxAutoSeverity: 99}
	tooHigh.Normalize()
	if tooHigh.MaxAutoSeverity > HighSeverityThreshold {
		t.Fatalf("ceiling = %d, must not exceed the high-severity threshold", tooHigh.MaxAutoSeverity)
	}
}

func TestReplySuggestionTrimsAndBounds(t *testing.T) {
	long := strings.Repeat("a", MaxReplyLength+200)
	s := NewReplySuggestion("row-1", "  "+long+"  ", "model-x")
	if len(s.Text) > MaxReplyLength {
		t.Fatalf("draft is %d runes, unbounded", len(s.Text))
	}
	if strings.HasPrefix(s.Text, " ") {
		t.Fatal("draft must be trimmed")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestReplySuggestionRejectsAnEmptyDraft(t *testing.T) {
	if err := NewReplySuggestion("row-1", "   ", "model-x").Validate(); err == nil {
		t.Fatal("an empty draft must not be publishable")
	}
}
