package callhistory_usecase

import (
	"context"

	"vozko/domain/callrouting"
	"vozko/domain/calls/billing"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/lead"
)

type facts struct {
	transfers map[string][]callrouting.TransferRecord
	charges   map[string]*billing.CallBillingRecord
	contacts  map[string]*lead.Lead
	names     map[string]string
}

func (h *History) gather(ctx context.Context, workspaceID string, calls []cdr.Call) (*facts, error) {
	gathered := &facts{transfers: map[string][]callrouting.TransferRecord{}, contacts: map[string]*lead.Lead{}}
	if len(calls) == 0 {
		return gathered, nil
	}
	callIDs := make([]string, len(calls))
	numbers := make([]string, 0, len(calls))
	for i, call := range calls {
		callIDs[i] = call.CallID
		numbers = append(numbers, callhistory.CounterpartNumber(call))
	}

	transfers, err := h.deps.Transfers.ForCalls(ctx, workspaceID, callIDs)
	if err != nil {
		return nil, err
	}
	for _, transfer := range transfers {
		gathered.transfers[transfer.CallID] = append(gathered.transfers[transfer.CallID], transfer)
	}
	if gathered.charges, err = h.deps.Charges.GetByCallIDs(callIDs); err != nil {
		return nil, err
	}
	contacts, err := h.deps.Contacts.FindByNumbers(workspaceID, numbers)
	if err != nil {
		return nil, err
	}
	for _, contact := range contacts {
		for _, format := range lead.NumberFormats(contact.Number) {
			gathered.contacts[format] = contact
		}
	}
	gathered.names = h.deps.Names.ResolveUsernames(peopleIn(calls, transfers))
	return gathered, nil
}

func peopleIn(calls []cdr.Call, transfers []callrouting.TransferRecord) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, call := range calls {
		if call.AgentID != nil {
			add(*call.AgentID)
		}
	}
	for _, transfer := range transfers {
		add(transfer.FromUserID)
		add(transfer.Target.UserID)
		add(transfer.AnsweredBy)
	}
	return ids
}

func (f *facts) summary(call cdr.Call) callhistory.Summary {
	transfers := f.transfers[call.CallID]
	people := callhistory.ParticipantsOf(call, transfers)
	summary := callhistory.Summary{
		CallID:      call.CallID,
		Direction:   call.Direction,
		Channel:     callhistory.ChannelOf(call),
		Outcome:     callhistory.OutcomeOf(call),
		StartedAt:   call.StartedAt,
		AnsweredAt:  call.AnsweredAt,
		EndedAt:     call.EndedAt,
		TalkSeconds: callhistory.TalkSeconds(call),
		RingSeconds: callhistory.RingSeconds(call),
		Contact:     f.contact(callhistory.CounterpartNumber(call)),
		PlacedBy:    f.personOrNil(people.PlacedBy),
		AnsweredBy:  f.personOrNil(people.AnsweredBy),
		Transfers:   len(transfers),
	}
	if call.EndReason != nil {
		summary.EndReason = *call.EndReason
	}
	if record, ok := f.charges[call.CallID]; ok && record != nil {
		summary.Charge = &callhistory.Charge{Micros: record.TotalRevenueMicros, Settled: record.Status == billing.StatusCharged}
	}
	return summary
}

func (f *facts) contact(number string) callhistory.Contact {
	contact := callhistory.Contact{Number: number}
	for _, format := range lead.NumberFormats(number) {
		if found, ok := f.contacts[format]; ok {
			contact.LeadID, contact.Name = found.ID, found.Name
			break
		}
	}
	return contact
}

func (f *facts) person(id string) callhistory.Person {
	return callhistory.Person{ID: id, Name: f.names[id]}
}

func (f *facts) personOrNil(id string) *callhistory.Person {
	if id == "" {
		return nil
	}
	person := f.person(id)
	return &person
}
