package conversation

import (
	"time"

	"vozko/domain/shared"
)

type InboundConsumer string

const (
	InboundConsumerRun   InboundConsumer = "run"
	InboundConsumerAgent InboundConsumer = "agent"
	InboundConsumerNone  InboundConsumer = "none"
)

type InboundFacts struct {
	AutomationAllowed    bool
	WaitingRun           bool
	WaitingRunUnreadable bool
	ResumedByForm        bool
}

func DecideInboundConsumer(f InboundFacts) InboundConsumer {
	switch {
	case !f.AutomationAllowed:
		return InboundConsumerNone
	case f.ResumedByForm:
		return InboundConsumerRun
	case f.WaitingRunUnreadable:
		return InboundConsumerNone
	case f.WaitingRun:
		return InboundConsumerRun
	}
	return InboundConsumerAgent
}

func (c InboundConsumer) AgentReplies() bool { return c == InboundConsumerAgent }

func (c InboundConsumer) StartsRuns() bool {
	return c == InboundConsumerAgent || c == InboundConsumerRun
}

type CustomerSpeech interface {
	CustomerSpokeAfter(entryID string, entryType shared.EntryType, after time.Time) (bool, error)
}
