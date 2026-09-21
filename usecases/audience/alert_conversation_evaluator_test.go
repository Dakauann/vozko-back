package audience_usecase

import (
	"context"
	"testing"
	"time"

	ca "vozko/domain/audience"
)

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
	if !contains(sent[0].Text, "41") {
		t.Errorf("the alert reported something other than the worst conversation: %q", sent[0].Text)
	}
}

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

func TestAlertTellsYouAboutOneConversationOnce(t *testing.T) {
	state := newFakeState()
	rules := &fakeAlertRules{rules: []*ca.AlertRule{qualityRule(0)}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, state)

	batch := []*ca.Analysis{conversationAt("conv-bad", 41, 30, now)}
	for i := 0; i < 3; i++ {
		rules.rules[0].LastFiredAt = nil
		rules.rules[0].FiredToday = 0
		uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1", batch)
	}
	if sent := dispatcher.all(); len(sent) != 1 {
		t.Fatalf("the same conversation raised %d alerts, want 1", len(sent))
	}

	rules.rules[0].LastFiredAt = nil
	rules.rules[0].FiredToday = 0
	uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1",
		[]*ca.Analysis{conversationAt("conv-other", 39, 30, now)})
	if sent := dispatcher.all(); len(sent) != 2 {
		t.Fatalf("a second bad conversation was suppressed: %d alerts", len(sent))
	}
}

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

func TestAlertWildcardRuleWatchesEveryChannel(t *testing.T) {
	rule := qualityRule(0)
	rule.Source = ""
	rule.Normalize()
	if err := rule.Validate(); err != nil {
		t.Fatalf("the wildcard rule is invalid: %v", err)
	}

	rules := &fakeAlertRules{rules: []*ca.AlertRule{rule}}
	dispatcher := &fakeDispatcher{}
	uc := conversationEvaluator(rules, dispatcher, newFakeState())

	telegram := ca.ContainerRef{
		Kind: ca.SubjectKindConversation, Source: ca.SourceTelegram,
		AccountID: "ws-1", ContainerID: "acc-1",
	}
	row := conversationAt("conv-tg", 41, 30, now)
	row.Source = ca.SourceTelegram

	uc.EvaluateBatch(context.Background(), telegram, "ws-1", []*ca.Analysis{row})

	if len(dispatcher.all()) != 1 {
		t.Fatalf("a wildcard rule sent %d alerts on Telegram, want 1", len(dispatcher.all()))
	}
}

func TestAlertWildcardAndScopedRulesCoexist(t *testing.T) {
	all := qualityRule(0)
	all.ID, all.Source, all.Name = "rule-all", "", "Qualquer canal"
	all.Normalize()

	onlyUnofficial := qualityRule(0)
	onlyUnofficial.ID, onlyUnofficial.Source, onlyUnofficial.Name = "rule-uw", ca.SourceUnofficialWhatsApp, "Só não oficial"
	onlyUnofficial.Normalize()

	for _, r := range []*ca.AlertRule{all, onlyUnofficial} {
		if err := r.Validate(); err != nil {
			t.Fatalf("%s is invalid: %v", r.Name, err)
		}
	}

	t.Run("both fire on the channel the scoped rule names", func(t *testing.T) {
		rules := &fakeAlertRules{rules: []*ca.AlertRule{all, onlyUnofficial}, claimLimit: 0}
		d := &fakeDispatcher{}
		uc := conversationEvaluator(rules, d, newFakeState())

		uc.EvaluateBatch(context.Background(), conversationRef(), "ws-1",
			[]*ca.Analysis{conversationAt("conv-bad", 41, 30, now)})

		if len(d.all()) != 2 {
			t.Fatalf("sent %d alerts on unofficial whatsapp, want both rules to fire", len(d.all()))
		}
	})

	t.Run("only the wildcard fires on a channel it does not name", func(t *testing.T) {
		all.LastFiredAt, all.FiredToday = nil, 0
		onlyUnofficial.LastFiredAt, onlyUnofficial.FiredToday = nil, 0
		rules := &fakeAlertRules{rules: []*ca.AlertRule{all, onlyUnofficial}}
		d := &fakeDispatcher{}
		uc := conversationEvaluator(rules, d, newFakeState())

		telegram := ca.ContainerRef{
			Kind: ca.SubjectKindConversation, Source: ca.SourceTelegram,
			AccountID: "ws-1", ContainerID: "acc-1",
		}
		row := conversationAt("conv-tg", 41, 30, now)
		row.Source = ca.SourceTelegram

		uc.EvaluateBatch(context.Background(), telegram, "ws-1", []*ca.Analysis{row})

		sent := d.all()
		if len(sent) != 1 {
			t.Fatalf("sent %d alerts on telegram, want only the wildcard", len(sent))
		}
		if !contains(sent[0].Text, "Qualquer canal") {
			t.Errorf("the channel-specific rule fired on a channel it does not watch: %q", sent[0].Text)
		}
	})
}
