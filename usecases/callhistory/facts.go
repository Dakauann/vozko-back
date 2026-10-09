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
	holders   map[string][]*lead.Lead
	linked    map[string]*lead.Lead
	names     map[string]string
}

func (h *History) gather(ctx context.Context, workspaceID string, calls []cdr.Call) (*facts, error) {
	gathered := &facts{transfers: map[string][]callrouting.TransferRecord{}, holders: map[string][]*lead.Lead{}, linked: map[string]*lead.Lead{}}
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
		for _, number := range contact.Numbers() {
			for _, format := range lead.NumberFormats(number) {
				gathered.holders[format] = append(gathered.holders[format], contact)
			}
		}
	}
	linked, err := h.deps.Contacts.FindByIDs(workspaceID, linkedLeadIDs(calls))
	if err != nil {
		return nil, err
	}
	for _, l := range linked {
		if l != nil && l.WorkspaceID == workspaceID {
			gathered.linked[l.ID] = l
		}
	}
	gathered.names = h.deps.Names.ResolveUsernames(peopleIn(calls, transfers))
	return gathered, nil
}

func linkedLeadIDs(calls []cdr.Call) []string {
	seen := map[string]bool{}
	var ids []string
	for _, call := range calls {
		if call.LeadID != nil && *call.LeadID != "" && !seen[*call.LeadID] {
			seen[*call.LeadID] = true
			ids = append(ids, *call.LeadID)
		}
	}
	return ids
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
		Contact:     f.contact(call),
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

func (f *facts) contact(call cdr.Call) callhistory.Contact {
	number := callhistory.CounterpartNumber(call)
	var linked *lead.Lead
	if call.LeadID != nil {
		linked = f.linked[*call.LeadID]
	}
	seen := map[*lead.Lead]bool{}
	var candidates []*lead.Lead
	for _, format := range lead.NumberFormats(number) {
		for _, holder := range f.holders[format] {
			if !seen[holder] {
				seen[holder] = true
				candidates = append(candidates, holder)
			}
		}
	}
	return callhistory.LinkedContact(number, linked, candidates)
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
