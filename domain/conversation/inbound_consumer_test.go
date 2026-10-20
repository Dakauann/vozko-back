package conversation

import (
	"testing"
)

func TestEachInboundMessageHasExactlyOneAutomatedConsumer(t *testing.T) {
	cases := []struct {
		name  string
		facts InboundFacts
		want  InboundConsumer
	}{
		{"a run waiting for a reply takes the message so the agent stays quiet", InboundFacts{AutomationAllowed: true, WaitingRun: true}, InboundConsumerRun},
		{"a form answer that already resumed a run belongs to that run", InboundFacts{AutomationAllowed: true, ResumedByForm: true}, InboundConsumerRun},
		{"without a waiting run the agent answers as before", InboundFacts{AutomationAllowed: true}, InboundConsumerAgent},
		{"automation switched off leaves the message to people", InboundFacts{WaitingRun: true}, InboundConsumerNone},
		{"automation switched off ignores a resumed form too", InboundFacts{ResumedByForm: true}, InboundConsumerNone},
		{"an unreadable waiting run silences both the run and the agent", InboundFacts{AutomationAllowed: true, WaitingRunUnreadable: true}, InboundConsumerNone},
		{"an unreadable lookup wins over a stale waiting flag", InboundFacts{AutomationAllowed: true, WaitingRun: true, WaitingRunUnreadable: true}, InboundConsumerNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideInboundConsumer(tc.facts); got != tc.want {
				t.Fatalf("DecideInboundConsumer(%+v) = %q, want %q", tc.facts, got, tc.want)
			}
		})
	}
}

func TestOnlyTheAgentConsumerLetsTheAgentReply(t *testing.T) {
	for consumer, want := range map[InboundConsumer]bool{
		InboundConsumerAgent: true,
		InboundConsumerRun:   false,
		InboundConsumerNone:  false,
		"":                   false,
	} {
		if got := consumer.AgentReplies(); got != want {
			t.Errorf("%q.AgentReplies() = %v, want %v", consumer, got, want)
		}
	}
}

func TestNewRunsMayStartUnlessTheMessageIsUnowned(t *testing.T) {
	for consumer, want := range map[InboundConsumer]bool{
		InboundConsumerAgent: true,
		InboundConsumerRun:   true,
		InboundConsumerNone:  false,
		"":                   false,
	} {
		if got := consumer.StartsRuns(); got != want {
			t.Errorf("%q.StartsRuns() = %v, want %v", consumer, got, want)
		}
	}
}
