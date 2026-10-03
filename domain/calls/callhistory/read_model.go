package callhistory

import (
	"time"

	"vozko/domain/calls/cdr"
)

type Person struct {
	ID   string
	Name string
}

type Contact struct {
	Number string
	LeadID string
	Name   string
}

type Charge struct {
	Micros  int64
	Settled bool
}

type Summary struct {
	CallID      string
	Direction   cdr.Direction
	Channel     Channel
	Outcome     Outcome
	EndReason   string
	StartedAt   time.Time
	AnsweredAt  *time.Time
	EndedAt     *time.Time
	TalkSeconds int
	RingSeconds int
	Contact     Contact
	PlacedBy    *Person
	AnsweredBy  *Person
	Transfers   int
	Charge      *Charge
}

type NamedEntry struct {
	TimelineEntry
	Actor     *Person
	Target    *Person
	QueueName string
}

type Recording struct {
	URL         string
	DurationSec int
}

type Detail struct {
	Summary
	Handlers  []Person
	Timeline  []NamedEntry
	Recording *Recording
}

func SourceOf(channel Channel) cdr.Source {
	if channel == ChannelWhatsApp {
		return cdr.SourceWhatsApp
	}
	return cdr.SourceSIPTrunk
}
