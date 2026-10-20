package conversation

import (
	"testing"
	"time"
)

func TestOnlyAPersonOrAnAutomationTalkingToTheCustomerAnswersThem(t *testing.T) {
	cases := []struct {
		name    string
		sender  SentBy
		answers bool
	}{
		{"an operator reply answers", SentByPerson("user-1"), true},
		{"an AI agent reply answers, as Zendesk and Front count bot replies", SentByAI("agent-1"), true},
		{"a workflow reply answers", SentByWorkflow("wf-1"), true},
		{"a reply typed on the phone outside the platform answers", SentExternally(), true},
		{"a campaign blast is not an answer to what the customer asked", SentByCampaign("camp-1"), false},
		{"a system notice is not an answer", SentBySystem(), false},
		{"the customer never answers themselves", SentByContact("5511"), false},
		{"an unknown sender answers nothing", RestoreSentBy("robot", "x"), false},
		{"an operator without an id answers nothing", SentByPerson(""), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.sender.AnswersTheCustomer(); got != c.answers {
				t.Fatalf("AnswersTheCustomer() = %v, want %v", got, c.answers)
			}
		})
	}
}

func TestEveryReplySenderKindAnswersTheCustomer(t *testing.T) {
	for _, kind := range ReplySenderKinds() {
		sender := RestoreSentBy(string(kind), "")
		switch kind {
		case SenderHuman:
			sender = SentByPerson("user-1")
		case SenderAI:
			sender = SentByAI("agent-1")
		case SenderWorkflow:
			sender = SentByWorkflow("wf-1")
		}
		if !sender.AnswersTheCustomer() {
			t.Errorf("%s is a reply sender kind, so the stored wait and the reply metrics would disagree", kind)
		}
	}
}

func TestWaitingSinceIsTheStoredWaitUnlessTheConversationIsFinished(t *testing.T) {
	since := time.Date(2026, 10, 19, 9, 30, 0, 0, time.FixedZone("BRT", -3*3600))
	cases := []struct {
		name   string
		status ConversationStatus
		since  *time.Time
		want   *time.Time
	}{
		{"a new conversation waits since the first unanswered message", ConversationStatusNew, &since, &since},
		{"an ongoing conversation waits since the first unanswered message", ConversationStatusOngoing, &since, &since},
		{"a conversation without a status still waits", "", &since, &since},
		{"a finished conversation waits for nobody", ConversationStatusFinished, &since, nil},
		{"nothing stored means nobody is waiting", ConversationStatusOngoing, nil, nil},
		{"a zero time means nobody is waiting", ConversationStatusOngoing, &time.Time{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WaitingSince(c.status, c.since)
			switch {
			case c.want == nil && got != nil:
				t.Fatalf("WaitingSince = %v, want nothing", *got)
			case c.want != nil && (got == nil || !got.Equal(*c.want)):
				t.Fatalf("WaitingSince = %v, want %v", got, *c.want)
			case got != nil && got.Location() != time.UTC:
				t.Fatalf("WaitingSince must be UTC on the wire, got %v", got.Location())
			}
		})
	}
}

func TestFinishingAConversationEndsTheWait(t *testing.T) {
	cases := map[ConversationStatus]bool{
		ConversationStatusFinished: true,
		ConversationStatusOngoing:  false,
		ConversationStatusNew:      false,
		"":                         false,
	}
	for status, ends := range cases {
		if got := status.EndsTheWait(); got != ends {
			t.Errorf("%q.EndsTheWait() = %v, want %v", status, got, ends)
		}
	}
}
