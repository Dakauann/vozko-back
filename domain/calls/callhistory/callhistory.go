package callhistory

import (
	"slices"

	"vozko/domain/callrouting"
	"vozko/domain/calls/cdr"
)

type Outcome string

const (
	OutcomeInProgress Outcome = "in_progress"
	OutcomeAnswered   Outcome = "answered"
	OutcomeMissed     Outcome = "missed"
	OutcomeNoAnswer   Outcome = "no_answer"
	OutcomeBusy       Outcome = "busy"
	OutcomeDeclined   Outcome = "declined"
	OutcomeCancelled  Outcome = "cancelled"
	OutcomeFailed     Outcome = "failed"
)

type Channel string

const (
	ChannelWhatsApp Channel = "whatsapp"
	ChannelPhone    Channel = "phone"
)

func OutcomeOf(call cdr.Call) Outcome {
	switch {
	case !call.IsTerminal():
		return OutcomeInProgress
	case call.AnsweredAt != nil:
		return OutcomeAnswered
	case call.Direction == cdr.DirectionInbound:
		return OutcomeMissed
	}
	switch endReason(call) {
	case string(OutcomeBusy):
		return OutcomeBusy
	case string(OutcomeDeclined):
		return OutcomeDeclined
	case string(OutcomeNoAnswer):
		return OutcomeNoAnswer
	case cdr.EndReasonCancelled:
		return OutcomeCancelled
	}
	return OutcomeFailed
}

func ChannelOf(call cdr.Call) Channel {
	if call.Source == cdr.SourceWhatsApp || cdr.IsWhatsAppCallID(call.CallID) {
		return ChannelWhatsApp
	}
	return ChannelPhone
}

func CounterpartNumber(call cdr.Call) string {
	if call.Direction == cdr.DirectionInbound {
		return call.PhoneFrom
	}
	return call.PhoneTo
}

func TalkSeconds(call cdr.Call) int {
	if call.AnsweredAt == nil || call.EndedAt == nil || !call.EndedAt.After(*call.AnsweredAt) {
		return 0
	}
	return int(call.EndedAt.Sub(*call.AnsweredAt).Seconds())
}

func RingSeconds(call cdr.Call) int {
	until := call.AnsweredAt
	if until == nil {
		until = call.EndedAt
	}
	if until == nil || !until.After(call.StartedAt) {
		return 0
	}
	return int(until.Sub(call.StartedAt).Seconds())
}

type Participants struct {
	PlacedBy   string
	AnsweredBy string
	Handlers   []string
	involved   []string
}

func (p Participants) Includes(userID string) bool {
	return userID != "" && slices.Contains(p.involved, userID)
}

func Visible(userID string, seesEveryone bool, call cdr.Call, transfers []callrouting.TransferRecord) bool {
	return seesEveryone || ParticipantsOf(call, transfers).Includes(userID)
}

func ParticipantsOf(call cdr.Call, transfers []callrouting.TransferRecord) Participants {
	var people Participants
	agent := valueOf(call.AgentID)
	if call.Direction == cdr.DirectionOutbound {
		people.PlacedBy = agent
	}
	if call.AnsweredAt != nil && agent != "" {
		people.Handlers = append(people.Handlers, agent)
	}
	people.involve(agent)
	for _, transfer := range chronological(transfers) {
		people.involve(transfer.FromUserID)
		people.involve(transfer.Target.UserID)
		if transfer.Outcome == callrouting.OutcomeConnected && transfer.AnsweredBy != "" {
			people.involve(transfer.AnsweredBy)
			if len(people.Handlers) == 0 || people.Handlers[len(people.Handlers)-1] != transfer.AnsweredBy {
				people.Handlers = append(people.Handlers, transfer.AnsweredBy)
			}
		}
	}
	if len(people.Handlers) > 0 {
		people.AnsweredBy = people.Handlers[len(people.Handlers)-1]
	}
	return people
}

func (p *Participants) involve(userID string) {
	if userID != "" && !slices.Contains(p.involved, userID) {
		p.involved = append(p.involved, userID)
	}
}

func chronological(transfers []callrouting.TransferRecord) []callrouting.TransferRecord {
	ordered := slices.Clone(transfers)
	slices.SortStableFunc(ordered, func(a, b callrouting.TransferRecord) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return ordered
}

func endReason(call cdr.Call) string { return valueOf(call.EndReason) }

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
