package audience

import (
	"strings"
	"testing"
	"time"
)

// Alerts were written for Instagram comments and the vocabulary said so: every
// metric read a comment field, so a workspace that only runs conversations had
// an alert screen it could configure and nothing it could ever watch. These
// cases pin the second half of that vocabulary and the guards that keep it from
// costing money on a loop.

func TestAlertMetricsAreScopedToTheSubjectTheyCanMeasure(t *testing.T) {
	comment := AlertMetricsFor(SubjectKindComment)
	conversation := AlertMetricsFor(SubjectKindConversation)

	if len(comment) == 0 || len(conversation) == 0 {
		t.Fatalf("both subjects need metrics: comment=%d conversation=%d", len(comment), len(conversation))
	}
	// The two lists must not overlap. A metric that reads a comment's severity
	// cannot be armed on a conversation, and a picker offering it would produce
	// a rule that silently never fires.
	for _, c := range comment {
		for _, v := range conversation {
			if c == v {
				t.Errorf("%q is offered for both subjects", c)
			}
		}
	}
	// And together they are the whole set, so a metric added to the type but to
	// neither list is unreachable from any picker.
	if len(comment)+len(conversation) != len(AllAlertMetrics()) {
		t.Errorf("the per-subject lists (%d + %d) do not cover AllAlertMetrics (%d)",
			len(comment), len(conversation), len(AllAlertMetrics()))
	}
	for _, m := range AllAlertMetrics() {
		if !m.SubjectKind().Valid() {
			t.Errorf("%q has no subject kind", m)
		}
	}
}

func TestAttendanceQualityAlarmsOnTheWayDown(t *testing.T) {
	// "Avise quando o atendimento cair abaixo de 70" is the whole point of this
	// metric, so it has to share the direction acceptance_score already had.
	if !AlertMetricAttendanceQuality.TriggersWhenBelow() {
		t.Error("attendance quality must alarm when it FALLS")
	}
	if AlertMetricAttendanceQuality.IsWindowed() {
		t.Error("attendance quality is judged on one conversation, not a span")
	}
	r := AlertRule{Metric: AlertMetricAttendanceQuality, Threshold: 70}
	if !r.Crossed(57) {
		t.Error("a quality of 57 must cross a threshold of 70")
	}
	if r.Crossed(80) {
		t.Error("a quality of 80 must not cross a threshold of 70")
	}
}

// The minimum-message guard: "uma conversa com pelo menos X mensagens".
//
// A two-message conversation scores badly because it barely happened, not
// because it was handled badly. Without a floor, the first alert an operator
// ever receives is about a customer who said "oi" and left.
func TestAlertRuleAcceptsOnlyConversationsLongEnoughToJudge(t *testing.T) {
	r := AlertRule{Metric: AlertMetricAttendanceQuality, Threshold: 70, MinMessages: 10}

	short := &Analysis{SubjectKind: SubjectKindConversation, MessageCount: 4}
	long := &Analysis{SubjectKind: SubjectKindConversation, MessageCount: 25}

	if r.Accepts(short) {
		t.Error("a 4-message conversation passed a 10-message floor")
	}
	if !r.Accepts(long) {
		t.Error("a 25-message conversation was refused by a 10-message floor")
	}
	// Exactly at the floor counts: "pelo menos X" includes X.
	if !r.Accepts(&Analysis{SubjectKind: SubjectKindConversation, MessageCount: 10}) {
		t.Error("a conversation exactly at the floor was refused")
	}
	// No floor configured accepts anything, which is what every existing rule
	// does and must keep doing.
	if !(AlertRule{Metric: AlertMetricAttendanceQuality}).Accepts(short) {
		t.Error("a rule with no floor refused a short conversation")
	}
	// A windowed rule has no single row to measure, so the guard cannot apply.
	if !(AlertRule{Metric: AlertMetricEscalationCount, MinMessages: 50}).Accepts(nil) {
		t.Error("a windowed rule was blocked by a per-conversation guard")
	}
}

func TestAlertRuleValidatesTheConversationFields(t *testing.T) {
	base := func() AlertRule {
		return AlertRule{
			WorkspaceID: "ws-1", AccountID: "ws-1", Name: "Atendimento fraco",
			Source: SourceUnofficialWhatsApp, Metric: AlertMetricAttendanceQuality,
			Threshold: 70, Channel: AlertChannelUnofficial, Recipient: "+5511999999999",
		}
	}

	ok := base()
	ok.Normalize()
	if err := ok.Validate(); err != nil {
		t.Fatalf("a well-formed conversation rule was refused: %v", err)
	}

	t.Run("quality threshold is a percentage", func(t *testing.T) {
		bad := base()
		bad.Threshold = 140
		bad.Normalize()
		if err := bad.Validate(); err == nil {
			t.Error("a quality threshold of 140 was accepted")
		}
	})

	t.Run("a message floor on a windowed rule is refused", func(t *testing.T) {
		// Not silently zeroed: the operator asked for something the metric
		// cannot honour, and clearing it would leave them believing it applied.
		bad := base()
		bad.Metric = AlertMetricEscalationCount
		bad.Threshold = 3
		bad.MinMessages = 10
		bad.Normalize()
		if err := bad.Validate(); err == nil {
			t.Error("a per-conversation floor was accepted on a windowed rule")
		}
	})

	t.Run("a comment metric on a conversation source is refused", func(t *testing.T) {
		bad := base()
		bad.Metric = AlertMetricCommentSeverity
		bad.Threshold = 80
		bad.Normalize()
		if err := bad.Validate(); err == nil {
			t.Error("a comment metric was accepted on a channel that has no comments")
		}
	})
}

// The message a human reads has to name what happened in that subject's own
// words, or the alert is a number with no sentence around it.
func TestAlertHeadlineSpeaksAboutConversations(t *testing.T) {
	for _, m := range AlertMetricsFor(SubjectKindConversation) {
		rule := AlertRule{Name: "Regra", Metric: m, Threshold: 70, WindowMinutes: 60}
		a := NewAlert(rule, AlertObservation{Metric: m, Value: 57}, time.Now())
		head := a.Headline()
		if head == "" || head == "Regra: 57" {
			t.Errorf("%q has no wording of its own: %q", m, head)
		}
	}
}

// The first line the recipient reads has to name the right thing. It said
// "Alerta de comentários" for every rule, including the ones watching WhatsApp.
func TestAlertMessageNamesTheSubjectItIsAbout(t *testing.T) {
	conv := NewAlert(
		AlertRule{Name: "Atendimento fraco", Metric: AlertMetricAttendanceQuality, Threshold: 70},
		AlertObservation{Metric: AlertMetricAttendanceQuality, Value: 41},
		time.Now(),
	)
	if got := conv.Message(); !strings.HasPrefix(got, "Alerta de conversas") {
		t.Errorf("a conversation alert opened with %q", strings.SplitN(got, "\n", 2)[0])
	}

	comment := NewAlert(
		AlertRule{Name: "Comentário grave", Metric: AlertMetricCommentSeverity, Threshold: 80},
		AlertObservation{Metric: AlertMetricCommentSeverity, Value: 92},
		time.Now(),
	)
	if got := comment.Message(); !strings.HasPrefix(got, "Alerta de comentários") {
		t.Errorf("a comment alert opened with %q", strings.SplitN(got, "\n", 2)[0])
	}
}
