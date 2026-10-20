package commentautomation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
)

type memoryClaims struct {
	byKey   map[string]string
	fail    error
	cutoffs []time.Time
}

func newMemoryClaims() *memoryClaims {
	return &memoryClaims{byKey: map[string]string{}}
}

func (m *memoryClaims) Claim(_ context.Context, source shared.EntryType, commentID, ruleID string) (bool, error) {
	if m.fail != nil {
		return false, m.fail
	}
	key := string(source) + "|" + commentID
	if _, taken := m.byKey[key]; taken {
		return false, nil
	}
	m.byKey[key] = ruleID
	return true, nil
}

func (m *memoryClaims) PurgeOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	m.cutoffs = append(m.cutoffs, cutoff)
	return 0, m.fail
}

type failingRules struct {
	ca.Repository
	err error
}

func (f failingRules) ListCandidates(context.Context, shared.EntryType, string, string) ([]*ca.Rule, error) {
	return nil, f.err
}

func evaluate(e *Evaluator, commentID, text string, actions Actions) error {
	return e.Evaluate(context.Background(), shared.EntryTypeFacebook, "page-1", commentID,
		ca.Subject{ContainerID: "p-1", AuthorName: "Ana", Text: text}, actions)
}

func TestARedeliveredCommentRunsNoActionTwice(t *testing.T) {
	claims := newMemoryClaims()
	evaluator := NewEvaluator(newMemoryRules(fbRule("r1", 1, "preço")), claims)
	actions := &recordingActions{}

	if err := evaluate(evaluator, "c-1", "preço?", actions); err != nil {
		t.Fatal(err)
	}
	first := len(actions.calls)
	if err := evaluate(evaluator, "c-1", "preço?", actions); err != nil {
		t.Fatalf("a redelivery is not a failure: %v", err)
	}
	if first != 3 || len(actions.calls) != first {
		t.Fatalf("calls = %v", actions.calls)
	}
	if claims.byKey["facebook|c-1"] != "r1" {
		t.Fatalf("claim must record the rule that ran: %v", claims.byKey)
	}
}

func TestAClaimErrorRunsNoActionAndAsksForARetry(t *testing.T) {
	claims := newMemoryClaims()
	claims.fail = errors.New("db down")
	actions := &recordingActions{}

	err := evaluate(NewEvaluator(newMemoryRules(fbRule("r1", 1, "preço")), claims), "c-1", "preço", actions)

	if len(actions.calls) != 0 {
		t.Fatalf("calls = %v", actions.calls)
	}
	if !errors.Is(err, ca.ErrExecutionUnclaimed) || !ca.Retryable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnavailableRulesAskForARetryInsteadOfDroppingTheComment(t *testing.T) {
	claims := newMemoryClaims()
	actions := &recordingActions{}

	err := evaluate(NewEvaluator(failingRules{err: errors.New("timeout")}, claims), "c-1", "preço", actions)

	if !errors.Is(err, ca.ErrRulesUnavailable) || !ca.Retryable(err) {
		t.Fatalf("err = %v", err)
	}
	if len(actions.calls) != 0 || len(claims.byKey) != 0 {
		t.Fatalf("calls %v claims %v", actions.calls, claims.byKey)
	}
}

func TestACommentNoRuleMatchesTakesNoClaim(t *testing.T) {
	claims := newMemoryClaims()
	if err := evaluate(NewEvaluator(newMemoryRules(fbRule("r1", 1, "preço")), claims), "c-1", "que lindo", &recordingActions{}); err != nil {
		t.Fatal(err)
	}
	if len(claims.byKey) != 0 {
		t.Fatalf("an unmatched comment stays free for a later edit: %v", claims.byKey)
	}
}

func TestEveryActionIsAttemptedAndEveryFailureReported(t *testing.T) {
	actions := &recordingActions{fail: map[ca.Action]error{
		ca.ActionPrivateReply: errors.New("graph 500"),
		ca.ActionLike:         errors.New("graph 400"),
	}}

	err := evaluate(NewEvaluator(newMemoryRules(fbRule("r1", 1, "preço")), newMemoryClaims()), "c-1", "preço", actions)

	if len(actions.calls) != 3 {
		t.Fatalf("calls = %v", actions.calls)
	}
	if !errors.Is(err, ca.ErrActionFailed) || ca.Retryable(err) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "graph 500") || !strings.Contains(err.Error(), "graph 400") {
		t.Fatalf("both failures must be reported: %v", err)
	}
}

func TestARenderedReplyOverTheLimitIsNotSent(t *testing.T) {
	rule := fbRule("r1", 1, "preço")
	rule.PublicReplyText = "{{comment}}"
	actions := &recordingActions{}

	err := evaluate(NewEvaluator(newMemoryRules(rule), newMemoryClaims()), "c-1", "preço "+strings.Repeat("a", 8000), actions)

	for _, call := range actions.calls {
		if strings.HasPrefix(call, string(ca.ActionPublicReply)) {
			t.Fatalf("an oversized reply reached the channel: %v", actions.calls)
		}
	}
	if len(actions.calls) != 2 || !errors.Is(err, ca.ErrReplyTooLong) || !errors.Is(err, ca.ErrActionFailed) {
		t.Fatalf("calls %v err %v", actions.calls, err)
	}
}

func TestACommentWithoutIDRunsNothing(t *testing.T) {
	claims := newMemoryClaims()
	actions := &recordingActions{}
	err := evaluate(NewEvaluator(newMemoryRules(fbRule("r1", 1, "preço")), claims), "  ", "preço", actions)
	if !errors.Is(err, ca.ErrCommentIDRequired) || ca.Retryable(err) || len(actions.calls) != 0 || len(claims.byKey) != 0 {
		t.Fatalf("err %v calls %v", err, actions.calls)
	}
}

func TestTheEvaluatorRequiresItsClaims(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an evaluator without execution claims must not be built")
		}
	}()
	NewEvaluator(newMemoryRules(), nil)
}

func TestExecutionPurgeKeepsTheRetentionWindow(t *testing.T) {
	claims := newMemoryClaims()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	purge := NewExecutionPurge(claims, func() time.Time { return now })

	if err := purge.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(claims.cutoffs) != 1 || !claims.cutoffs[0].Equal(now.Add(-ca.ExecutionRetention)) {
		t.Fatalf("cutoffs = %v", claims.cutoffs)
	}
	claims.fail = errors.New("db down")
	if err := purge.Execute(context.Background()); err == nil {
		t.Fatal("a failed purge must surface")
	}
}
