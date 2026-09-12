package audience_usecase

import (
	"context"
	"testing"
	"time"

	ca "vozko/domain/audience"
)

// The evaluator judging CONVERSATIONS. Same machinery as comments, a different
// subject, and three ways that could go wrong once money is involved.

func conversationRef() ca.ContainerRef {
	return ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceUnofficialWhatsApp,
		AccountID: "ws-1", ContainerID: "camp-1",
	}
}

func qualityRule(minMessages int) *ca.AlertRule {
	r := &ca.AlertRule{
		ID: "rule-q", WorkspaceID: "ws-1", Source: ca.SourceUnofficialWhatsApp, AccountID: "ws-1",
		Name: "Atendimento fraco", Metric: ca.AlertMetricAttendanceQuality, Threshold: 70,
		MinMessages: minMessages,
		Channel:     ca.AlertChannelUnofficial, Recipient: "5511999999999", Enabled: true,
	}
	r.Normalize()
	return r
}

func conversationAt(id string, quality, messages int, at time.Time) *ca.Analysis {
	return &ca.Analysis{
		ID: id, WorkspaceID: "ws-1", Source: ca.SourceUnofficialWhatsApp, AccountID: "ws-1",
		SubjectKind: ca.SubjectKindConversation, ContainerID: "camp-1", SubjectID: id,
		AuthorHandle: "fulano", Status: ca.StatusAnalyzed,
		AttendanceQuality: quality, MessageCount: messages,
		Summary: "cliente pediu orçamento e ninguém respondeu", OccurredAt: at,
	}
}

func conversationEvaluator(rules *fakeAlertRules, d *fakeDispatcher, state *fakeState) ca.AlertEvaluator {
	return NewAlertEvaluator(AlertDeps{
		Rules: rules, Repo: newFakeRepo(), State: state, Clock: fixedClock{now},
		Sender: NewAlertConsumer(AlertConsumerDeps{Dispatcher: d, Rules: rules, Clock: fixedClock{now}}),
	})
}

func TestAlertFiresOnAPoorlyHandledConversation(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(10)}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, newFakeState())

	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1", []*ca.Analysis{
		conversationAt("conv-ok", 88, 30, now),
		conversationAt("conv-bad", 41, 30, now),
	})

	sent := dispatcher.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d alerts, want 1", len(sent))
	}
	// The WORST conversation of the batch, which for this subject means the
	// least well handled one, not the most severe.
	if !contains(sent[0].Text, "41") {
		t.Errorf("the alert reported something other than the worst conversation: %q", sent[0].Text)
	}
}

// The floor from the requirement: "uma conversa com pelo menos X mensagens".
func TestAlertSkipsAConversationTooShortToJudge(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(10)}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, newFakeState())

	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1", []*ca.Analysis{
		conversationAt("conv-tiny", 10, 3, now),
	})

	if sent := dispatcher.all(); len(sent) != 0 {
		t.Fatalf("a 3-message conversation raised %d alerts: %+v", len(sent), sent)
	}
}

// A comment rule must not fire on a conversation batch, and the reverse. The
// two vocabularies share one evaluator, and crossing them produces an alert
// measuring a field the subject does not have.
func TestAlertRulesDoNotCrossSubjects(t *testing.T) {
	rules := &fakeAlertRules{rules: []*ca.AlertRule{severityRule()}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, newFakeState())

	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1", []*ca.Analysis{
		conversationAt("conv-bad", 5, 40, now),
	})

	if sent := dispatcher.all(); len(sent) != 0 {
		t.Fatalf("a comment rule fired on a conversation batch: %+v", sent)
	}
}

// The money guard. A conversation is re-analysed as it grows, so the same bad
// conversation reaches the evaluator again and again. Without a per-subject
// claim it would spend the rule's whole daily allowance on one customer and the
// next conversation to go wrong would be dropped in silence.
func TestAlertTellsYouAboutOneConversationOnce(t *testing.T) {
	state := newFakeState()
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(0)}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, state)

	batch := []*ca.Analysis{conversationAt("conv-bad", 41, 30, now)}
	for i := 0; i < 3; i++ {
		// Cleared between passes so the RULE's own cooldown is not what is
		// under test here; the per-subject claim is.
		rules.rules[0].LastFiredAt = nil
		rules.rules[0].FiredToday = 0
		uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1", batch)
	}
	if sent := dispatcher.all(); len(sent) != 1 {
		t.Fatalf("the same conversation raised %d alerts, want 1", len(sent))
	}

	// A DIFFERENT conversation still gets through: the dedupe is per subject,
	// not a second cooldown.
	rules.rules[0].LastFiredAt = nil
	rules.rules[0].FiredToday = 0
	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1",
		[]*ca.Analysis{conversationAt("conv-other", 39, 30, now)})
	if sent := dispatcher.all(); len(sent) != 2 {
		t.Fatalf("a second bad conversation was suppressed: %d alerts", len(sent))
	}
}

// A test message has to say it is a test, in more than the prefix.
//
// Its number is the rule's own threshold rather than a measurement, so without
// saying so it reads as a real verdict about a real conversation, and the
// missing AI reading reads as a broken feature rather than as "there is nothing
// here to read".
func TestAlertTestMessageSaysWhatItIsNot(t *testing.T) {
	plain := testFooter(ca.AlertRule{Metric: ca.AlertMetricAttendanceQuality})
	if !contains(plain, "não uma medição") {
		t.Errorf("the footer does not say the number is not a measurement: %q", plain)
	}
	if contains(plain, "IA") {
		t.Error("a rule without a briefing mentioned the AI reading")
	}

	briefed := testFooter(ca.AlertRule{Metric: ca.AlertMetricAttendanceQuality, Brief: true})
	if !contains(briefed, "IA") {
		t.Error("a rule WITH a briefing did not explain why the AI reading is absent")
	}
}
