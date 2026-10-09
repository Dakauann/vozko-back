package callhistory_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/calls/billing"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/calls/recordings"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

var start = time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

type callBook struct {
	calls   []*cdr.Call
	filters cdr.ListFilters
}

func (b *callBook) List(filters cdr.ListFilters) (*shared.PaginatedResult[*cdr.Call], error) {
	b.filters = filters
	pagination := shared.NormalizePagination(filters.Pagination)
	return shared.NewPaginatedResult(b.calls, pagination, int64(len(b.calls))), nil
}

func (b *callBook) GetByCallID(callID string) (*cdr.Call, error) {
	for _, call := range b.calls {
		if call.CallID == callID {
			return call, nil
		}
	}
	return nil, cdr.ErrCallNotFound
}

type transferBook []callrouting.TransferRecord

func (b transferBook) ForCalls(_ context.Context, workspaceID string, callIDs []string) ([]callrouting.TransferRecord, error) {
	var out []callrouting.TransferRecord
	for _, record := range b {
		for _, id := range callIDs {
			if record.CallID == id && record.WorkspaceID == workspaceID {
				out = append(out, record)
			}
		}
	}
	return out, nil
}

type chargeBook map[string]*billing.CallBillingRecord

func (b chargeBook) GetByCallIDs(callIDs []string) (map[string]*billing.CallBillingRecord, error) {
	out := map[string]*billing.CallBillingRecord{}
	for _, id := range callIDs {
		if record, ok := b[id]; ok {
			out[id] = record
		}
	}
	return out, nil
}

type recordingBook map[string]*recordings.CallRecord

func (b recordingBook) GetByCallID(callID string) (*recordings.CallRecord, error) {
	return b[callID], nil
}

type contactBook []*lead.Lead

func (b contactBook) FindByNumbers(workspaceID string, numbers []string) ([]*lead.Lead, error) {
	var out []*lead.Lead
	for _, contact := range b {
		if contact.WorkspaceID == workspaceID {
			out = append(out, contact)
		}
	}
	return out, nil
}

func (b contactBook) FindByIDs(workspaceID string, ids []string) ([]*lead.Lead, error) {
	var out []*lead.Lead
	for _, contact := range b {
		for _, id := range ids {
			if contact.ID == id && contact.WorkspaceID == workspaceID {
				out = append(out, contact)
			}
		}
	}
	return out, nil
}

type nameBook map[string]string

func (b nameBook) ResolveUsernames(ids []string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := b[id]; ok {
			out[id] = name
		}
	}
	return out
}

type queueBook []*callrouting.Queue

func (b queueBook) ListByWorkspace(_ context.Context, workspaceID string) ([]*callrouting.Queue, error) {
	return b, nil
}

func fixture() (*History, *callBook) {
	return fixtureWith(contactBook{{ID: "lead-1", WorkspaceID: "ws1", Number: "558494409684", Name: "Maria"}})
}

func fixtureWith(contacts contactBook) (*History, *callBook) {
	answered := &cdr.Call{
		CallID: "sip-out-1", WorkspaceID: "ws1", Direction: cdr.DirectionOutbound, Source: cdr.SourceSIPTrunk,
		Status: cdr.StatusCompleted, PhoneTo: "5584994409684", AgentID: ptr("ana"),
		StartedAt: start, AnsweredAt: ptr(start.Add(8 * time.Second)), EndedAt: ptr(start.Add(128 * time.Second)), EndReason: ptr("ended"),
	}
	missed := &cdr.Call{
		CallID: "wa-in-2", WorkspaceID: "ws1", Direction: cdr.DirectionInbound, Source: cdr.SourceWhatsApp,
		Status: cdr.StatusFailed, PhoneFrom: "5511999990000", StartedAt: start.Add(-time.Hour), EndedAt: ptr(start.Add(-time.Hour + 30*time.Second)),
	}
	calls := &callBook{calls: []*cdr.Call{answered, missed}}
	history := NewHistory(Deps{
		Calls: calls,
		Transfers: transferBook{{
			ID: "t1", WorkspaceID: "ws1", CallID: "sip-out-1", FromUserID: "ana",
			Target: callrouting.TransferTarget{Kind: callrouting.TargetQueue, QueueID: "q1"}, Notes: "quer cancelar",
			Outcome: callrouting.OutcomeConnected, AnsweredBy: "bia", CreatedAt: start.Add(60 * time.Second), FinishedAt: start.Add(70 * time.Second),
		}},
		Charges:    chargeBook{"sip-out-1": {CallID: "sip-out-1", TotalRevenueMicros: 26_666, Status: billing.StatusCharged}},
		Recordings: recordingBook{"sip-out-1": {CallID: "sip-out-1", WorkspaceID: "ws1", RecordingURL: "https://files/rec.wav", DurationSec: 120, CreatedAt: start.Add(130 * time.Second)}},
		Contacts:   contacts,
		Names:      nameBook{"ana": "Ana", "bia": "Bia"},
		Queues:     queueBook{{ID: "q1", Name: "Suporte"}},
	})
	return history, calls
}

var manager = Viewer{WorkspaceID: "ws1", UserID: "boss", SeesEveryone: true, HearsRecordings: true}

func TestAnOperatorOnlyListsCallsTheyTookPartIn(t *testing.T) {
	history, calls := fixture()
	_, err := history.List(context.Background(), ListInput{Viewer: Viewer{WorkspaceID: "ws1", UserID: "ana"}, MemberID: "someone-else"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.filters.ParticipantID == nil || *calls.filters.ParticipantID != "ana" {
		t.Fatalf("participant filter = %v, want forced to the operator", calls.filters.ParticipantID)
	}
}

func TestAViewerWithoutAnIdentitySeesNothingRatherThanEverything(t *testing.T) {
	history, calls := fixture()
	_, _ = history.List(context.Background(), ListInput{Viewer: Viewer{WorkspaceID: "ws1"}})
	if calls.filters.ParticipantID == nil || *calls.filters.ParticipantID != "" {
		t.Fatalf("participant filter = %v, want a filter that matches nobody", calls.filters.ParticipantID)
	}
	if _, err := history.List(context.Background(), ListInput{Viewer: Viewer{UserID: "ana"}}); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("no workspace err = %v", err)
	}
}

func TestAManagerListsEveryoneAndMayNarrowToAMember(t *testing.T) {
	history, calls := fixture()
	_, _ = history.List(context.Background(), ListInput{Viewer: manager})
	if calls.filters.ParticipantID != nil {
		t.Fatal("a manager's list was narrowed without asking")
	}
	_, _ = history.List(context.Background(), ListInput{
		Viewer: manager, MemberID: "bia", Direction: ptr(cdr.DirectionInbound),
		Channel: ptr(callhistory.ChannelWhatsApp), Answered: ptr(false), Number: "(84) 99440-9684",
		From: ptr(start.Add(-24 * time.Hour)), To: ptr(start),
	})
	f := calls.filters
	if *f.ParticipantID != "bia" || *f.Direction != cdr.DirectionInbound || *f.Source != cdr.SourceWhatsApp || *f.Answered || *f.NumberDigits != "84994409684" || f.WorkspaceID != "ws1" || f.StartedFrom == nil || f.StartedTo == nil {
		t.Fatalf("filters = %+v", f)
	}
}

func TestTheListReadsLikeAPhoneLog(t *testing.T) {
	history, _ := fixture()
	page, err := history.List(context.Background(), ListInput{Viewer: manager})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.TotalItems != 2 {
		t.Fatalf("page = %+v", page)
	}
	answered := page.Items[0]
	if answered.Outcome != callhistory.OutcomeAnswered || answered.Channel != callhistory.ChannelPhone || answered.TalkSeconds != 120 || answered.RingSeconds != 8 {
		t.Fatalf("answered = %+v", answered)
	}
	if answered.Contact != (callhistory.Contact{Number: "5584994409684", LeadID: "lead-1", Name: "Maria", Holders: 1}) {
		t.Fatalf("contact = %+v, want Maria matched through the other mobile format", answered.Contact)
	}
	if answered.PlacedBy == nil || *answered.PlacedBy != (callhistory.Person{ID: "ana", Name: "Ana"}) || answered.AnsweredBy == nil || answered.AnsweredBy.Name != "Bia" {
		t.Fatalf("people = %+v / %+v", answered.PlacedBy, answered.AnsweredBy)
	}
	if answered.Transfers != 1 || answered.Charge == nil || answered.Charge.Micros != 26_666 || !answered.Charge.Settled {
		t.Fatalf("transfers %d charge %+v", answered.Transfers, answered.Charge)
	}
	missed := page.Items[1]
	if missed.Outcome != callhistory.OutcomeMissed || missed.Contact.Name != "" || missed.Contact.Number != "5511999990000" || missed.AnsweredBy != nil || missed.Charge != nil {
		t.Fatalf("missed = %+v", missed)
	}
}

func TestTheDetailTellsTheWholeStoryWithNames(t *testing.T) {
	history, _ := fixture()
	detail, err := history.Get(context.Background(), manager, "sip-out-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Handlers) != 2 || detail.Handlers[0].Name != "Ana" || detail.Handlers[1].Name != "Bia" {
		t.Fatalf("handlers = %+v", detail.Handlers)
	}
	var queued *callhistory.NamedEntry
	for i := range detail.Timeline {
		if detail.Timeline[i].Kind == callhistory.EntryTransferConnected {
			queued = &detail.Timeline[i]
		}
	}
	if queued == nil || queued.QueueName != "Suporte" || queued.Actor == nil || queued.Actor.Name != "Bia" {
		t.Fatalf("queue connection = %+v", queued)
	}
	if detail.Recording == nil || detail.Recording.URL != "https://files/rec.wav" || detail.Recording.DurationSec != 120 {
		t.Fatalf("recording = %+v", detail.Recording)
	}
}

func TestSomeoneWithoutRecordingAccessGetsNoRecording(t *testing.T) {
	history, _ := fixture()
	viewer := manager
	viewer.HearsRecordings = false
	detail, err := history.Get(context.Background(), viewer, "sip-out-1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Recording != nil {
		t.Fatal("the recording leaked to someone without call_recordings:read")
	}
	for _, entry := range detail.Timeline {
		if entry.Kind == callhistory.EntryRecordingReady {
			t.Fatal("the timeline revealed a recording the viewer may not hear")
		}
	}
}

func TestACallYouDidNotTakePartInDoesNotExistForYou(t *testing.T) {
	history, _ := fixture()
	for _, viewer := range []Viewer{
		{WorkspaceID: "ws1", UserID: "caio"},
		{WorkspaceID: "ws2", UserID: "ana", SeesEveryone: true},
	} {
		if _, err := history.Get(context.Background(), viewer, "sip-out-1"); !errors.Is(err, cdr.ErrCallNotFound) {
			t.Fatalf("viewer %+v err = %v, want not found", viewer, err)
		}
	}
	if _, err := history.Get(context.Background(), Viewer{WorkspaceID: "ws1", UserID: "bia"}, "sip-out-1"); err != nil {
		t.Fatalf("bia answered the transfer and must see the call: %v", err)
	}
}

func TestTheListFindsALeadThroughAContactPhoneAndNamesNobodyForASharedLine(t *testing.T) {
	history, _ := fixtureWith(contactBook{
		{ID: "lead-1", WorkspaceID: "ws1", Number: "558494409684", Name: "Maria"},
		{ID: "filho", WorkspaceID: "ws1", Name: "Filho", Phones: []lead.ContactPhone{{Number: "5511999990000"}, {Number: "5584994409684"}}},
		{ID: "vizinha", WorkspaceID: "ws1", Name: "Vizinha", Phones: []lead.ContactPhone{{Number: "5511999990000"}}},
	})
	page, err := history.List(context.Background(), ListInput{Viewer: manager})
	if err != nil {
		t.Fatal(err)
	}
	contacts := map[string]callhistory.Contact{}
	for _, item := range page.Items {
		contacts[item.CallID] = item.Contact
	}
	if got := contacts["sip-out-1"]; got.LeadID != "lead-1" || got.Holders != 2 {
		t.Fatalf("the WhatsApp owner is named even when a relative lists the number, got %+v", got)
	}
	if got := contacts["wa-in-2"]; got.LeadID != "" || got.Holders != 2 {
		t.Fatalf("a line two leads share names nobody, got %+v", got)
	}
}

func TestTheListNarrowsToOneLead(t *testing.T) {
	history, calls := fixture()
	if _, err := history.List(context.Background(), ListInput{Viewer: manager, LeadID: " lead-1 "}); err != nil {
		t.Fatal(err)
	}
	if calls.filters.LeadID == nil || *calls.filters.LeadID != "lead-1" {
		t.Fatalf("lead filter = %v, want lead-1", calls.filters.LeadID)
	}
	_, _ = history.List(context.Background(), ListInput{Viewer: manager})
	if calls.filters.LeadID != nil {
		t.Fatal("no lead asked, no lead filter")
	}
}

func TestACallLinkedToALeadNamesThatLeadAndOldCallsFallBackToTheNumber(t *testing.T) {
	history, calls := fixtureWith(contactBook{
		{ID: "lead-1", WorkspaceID: "ws1", Number: "558494409684", Name: "Maria"},
		{ID: "filho", WorkspaceID: "ws1", Name: "Filho", Phones: []lead.ContactPhone{{Number: "5584994409684"}}},
		{ID: "elsewhere", WorkspaceID: "ws2", Name: "Outro workspace"},
	})
	calls.calls[0].LeadID = ptr("filho")
	calls.calls[1].LeadID = ptr("elsewhere")
	page, err := history.List(context.Background(), ListInput{Viewer: manager})
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Items[0].Contact; got != (callhistory.Contact{Number: "5584994409684", LeadID: "filho", Name: "Filho", Holders: 2}) {
		t.Fatalf("linked contact = %+v, want the lead the call was placed for", got)
	}
	if got := page.Items[1].Contact; got.LeadID != "" || got.Name != "" {
		t.Fatalf("a link to a lead outside the workspace must name nobody, got %+v", got)
	}
}
