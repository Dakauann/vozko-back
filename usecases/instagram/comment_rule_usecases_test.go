package instagram

import (
	"context"
	"errors"
	"testing"

	igdomain "vozko/domain/instagram"
)

// recordingActions captures what the evaluator asked the provider to do.
type recordingActions struct {
	public    []string
	private   []string
	hidden    []string
	order     []string
	publicErr error
	privErr   error
}

func (a *recordingActions) ReplyPublicly(_ context.Context, _, _, commentID, message string) (string, error) {
	a.order = append(a.order, "public")
	if a.publicErr != nil {
		return "", a.publicErr
	}
	a.public = append(a.public, message)
	return "reply-1", nil
}

func (a *recordingActions) ReplyPrivately(_ context.Context, _, _, commentID, text string) error {
	a.order = append(a.order, "private")
	if a.privErr != nil {
		return a.privErr
	}
	a.private = append(a.private, text)
	return nil
}

func (a *recordingActions) SetHidden(_ context.Context, _, _, commentID string, hidden bool) error {
	a.order = append(a.order, "hide")
	a.hidden = append(a.hidden, commentID)
	return nil
}

// stubRules serves a fixed candidate list.
type stubRules struct {
	igdomain.CommentRuleRepository
	candidates []*igdomain.CommentRule
	err        error
	calls      int
}

func (s *stubRules) ListCandidates(_ context.Context, _, _ string) ([]*igdomain.CommentRule, error) {
	s.calls++
	return s.candidates, s.err
}

func rule(name string, actions ...igdomain.CommentRuleAction) *igdomain.CommentRule {
	r := &igdomain.CommentRule{
		ID: name, Name: name, Enabled: true,
		WorkspaceID: "ws-1", IGAccountID: "acct-1",
		Match: igdomain.MatchContains, Keywords: []string{"quero"},
		Actions:          actions,
		PublicReplyText:  "oi {{username}}",
		PrivateReplyText: "te chamei, {{username}}",
	}
	r.Normalize()
	return r
}

func inbound(text string) *igdomain.Comment {
	return &igdomain.Comment{
		IGCommentID: "c-1", IGMediaID: "m-1",
		IGAccountID: "acct-1", WorkspaceID: "ws-1",
		FromUsername: "maria", Text: text,
	}
}

func TestEvaluateRunsMatchingRuleActions(t *testing.T) {
	actions := &recordingActions{}
	uc := NewEvaluateCommentRulesUseCase(
		&stubRules{candidates: []*igdomain.CommentRule{
			rule("promo", igdomain.ActionPublicReply, igdomain.ActionPrivateReply),
		}},
		actions,
	)

	uc.Execute(context.Background(), inbound("eu quero!"))

	if len(actions.public) != 1 || actions.public[0] != "oi maria" {
		t.Errorf("public reply not rendered/sent: %v", actions.public)
	}
	if len(actions.private) != 1 || actions.private[0] != "te chamei, maria" {
		t.Errorf("private reply not rendered/sent: %v", actions.private)
	}
}

// Action order is the rule's own: replying after hiding would answer a comment
// nobody can see.
func TestEvaluatePreservesActionOrder(t *testing.T) {
	actions := &recordingActions{}
	uc := NewEvaluateCommentRulesUseCase(
		&stubRules{candidates: []*igdomain.CommentRule{
			rule("promo", igdomain.ActionPublicReply, igdomain.ActionPrivateReply, igdomain.ActionHide),
		}},
		actions,
	)

	uc.Execute(context.Background(), inbound("quero"))

	want := []string{"public", "private", "hide"}
	if len(actions.order) != len(want) {
		t.Fatalf("ran %v, want %v", actions.order, want)
	}
	for i := range want {
		if actions.order[i] != want[i] {
			t.Errorf("action %d = %q, want %q", i, actions.order[i], want[i])
		}
	}
}

// Only the first matching rule runs: a comment tripping both a promo rule and a
// spam rule must not be replied to AND hidden.
func TestEvaluateFirstMatchWins(t *testing.T) {
	actions := &recordingActions{}
	spam := rule("spam", igdomain.ActionHide)
	uc := NewEvaluateCommentRulesUseCase(
		&stubRules{candidates: []*igdomain.CommentRule{
			rule("promo", igdomain.ActionPublicReply),
			spam,
		}},
		actions,
	)

	uc.Execute(context.Background(), inbound("quero"))

	if len(actions.public) != 1 {
		t.Errorf("first rule should have replied: %v", actions.public)
	}
	if len(actions.hidden) != 0 {
		t.Error("the second matching rule must not also run")
	}
}

// Our own replies come back as webhooks; evaluating them would loop forever.
func TestEvaluateIgnoresOurOwnComments(t *testing.T) {
	actions := &recordingActions{}
	rules := &stubRules{candidates: []*igdomain.CommentRule{rule("promo", igdomain.ActionPublicReply)}}
	uc := NewEvaluateCommentRulesUseCase(rules, actions)

	own := inbound("quero")
	own.IsOurs = true
	uc.Execute(context.Background(), own)

	if rules.calls != 0 {
		t.Error("rules must not even be loaded for our own comment")
	}
	if len(actions.order) != 0 {
		t.Errorf("no action may run on our own comment: %v", actions.order)
	}
}

// The one-per-comment private reply allowance being spent is an expected
// outcome, not a fault: the remaining actions must still run.
func TestEvaluateContinuesWhenPrivateReplyAlreadyUsed(t *testing.T) {
	actions := &recordingActions{privErr: igdomain.ErrPrivateReplyUsed}
	uc := NewEvaluateCommentRulesUseCase(
		&stubRules{candidates: []*igdomain.CommentRule{
			rule("promo", igdomain.ActionPrivateReply, igdomain.ActionHide),
		}},
		actions,
	)

	uc.Execute(context.Background(), inbound("quero"))

	if len(actions.hidden) != 1 {
		t.Error("a spent private-reply allowance must not abort the remaining actions")
	}
}

// One provider failure must not cancel the rest: hiding spam is still worth
// doing when the reply failed.
func TestEvaluateContinuesAfterActionFailure(t *testing.T) {
	actions := &recordingActions{publicErr: errors.New("rate limited")}
	uc := NewEvaluateCommentRulesUseCase(
		&stubRules{candidates: []*igdomain.CommentRule{
			rule("promo", igdomain.ActionPublicReply, igdomain.ActionHide),
		}},
		actions,
	)

	uc.Execute(context.Background(), inbound("quero"))

	if len(actions.hidden) != 1 {
		t.Error("a failed action must not abort the rule")
	}
}

func TestEvaluateNoMatchDoesNothing(t *testing.T) {
	actions := &recordingActions{}
	uc := NewEvaluateCommentRulesUseCase(
		&stubRules{candidates: []*igdomain.CommentRule{rule("promo", igdomain.ActionPublicReply)}},
		actions,
	)

	uc.Execute(context.Background(), inbound("que lindo!"))

	if len(actions.order) != 0 {
		t.Errorf("no rule matched, nothing should run: %v", actions.order)
	}
}

// A repository failure must be survivable: the comment is already mirrored, and
// failing here would only trigger a webhook redelivery.
func TestEvaluateSurvivesRepositoryFailure(t *testing.T) {
	actions := &recordingActions{}
	uc := NewEvaluateCommentRulesUseCase(&stubRules{err: errors.New("db down")}, actions)

	uc.Execute(context.Background(), inbound("quero"))

	if len(actions.order) != 0 {
		t.Error("nothing should run when rules cannot be loaded")
	}
}

func TestEvaluateNilSafety(t *testing.T) {
	// Unconfigured evaluator (channel wired without rules) and nil comment must
	// both be no-ops rather than panics.
	(&EvaluateCommentRulesUseCase{}).Execute(context.Background(), inbound("quero"))
	NewEvaluateCommentRulesUseCase(&stubRules{}, &recordingActions{}).Execute(context.Background(), nil)
}
