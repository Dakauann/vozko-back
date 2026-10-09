package callhistory

import (
	"time"

	"vozko/domain/calls/callhistory"
	"vozko/domain/shared"
)

type CallPersonResponse struct {
	ID   string `json:"id" example:"7c9e6679-7425-40de-944b-e07fc1f90ae7"`
	Name string `json:"name" example:"Ana"`
}

type CallContactResponse struct {
	Number string `json:"number" example:"5584994409684"`
	LeadID string `json:"leadId,omitempty" example:"3f2b8c1e-5d4a-4b7e-9c1f-2a6d8e0b4c3a"`
	Name   string `json:"name,omitempty" example:"Maria Souza"`
	Leads  int    `json:"leads" example:"1"`
}

type CallChargeResponse struct {
	AmountMicros int64 `json:"amountMicros" example:"26666"`
	Settled      bool  `json:"settled" example:"true"`
}

type CallSummaryResponse struct {
	CallID      string              `json:"callId" example:"sip-out-2f1c9a7e-8d3b-4e6f-a1c2-9b8d7e6f5a4c"`
	Direction   string              `json:"direction" enums:"inbound,outbound" example:"outbound"`
	Channel     string              `json:"channel" enums:"phone,whatsapp" example:"phone"`
	Outcome     string              `json:"outcome" enums:"in_progress,answered,missed,no_answer,busy,declined,cancelled,failed" example:"answered"`
	EndReason   string              `json:"endReason,omitempty" example:"ended"`
	StartedAt   time.Time           `json:"startedAt" example:"2026-09-30T14:00:00Z"`
	AnsweredAt  *time.Time          `json:"answeredAt,omitempty" example:"2026-09-30T14:00:08Z"`
	EndedAt     *time.Time          `json:"endedAt,omitempty" example:"2026-09-30T14:02:08Z"`
	TalkSeconds int                 `json:"talkSeconds" example:"120"`
	RingSeconds int                 `json:"ringSeconds" example:"8"`
	Contact     CallContactResponse `json:"contact"`
	PlacedBy    *CallPersonResponse `json:"placedBy,omitempty"`
	AnsweredBy  *CallPersonResponse `json:"answeredBy,omitempty"`
	Transfers   int                 `json:"transfers" example:"1"`
	Charge      *CallChargeResponse `json:"charge,omitempty"`
}

type CallListResponse struct {
	Items      []CallSummaryResponse `json:"items"`
	Page       int                   `json:"page" example:"1"`
	PageSize   int                   `json:"pageSize" example:"25"`
	TotalItems int64                 `json:"totalItems" example:"132"`
	TotalPages int                   `json:"totalPages" example:"6"`
}

type CallTimelineEntryResponse struct {
	Kind      string              `json:"kind" enums:"started,answered,transfer_requested,transfer_connected,transfer_returned,transfer_unanswered,transfer_cancelled,caller_left,ended,recording_ready" example:"transfer_connected"`
	At        time.Time           `json:"at" example:"2026-09-30T14:01:10Z"`
	Actor     *CallPersonResponse `json:"actor,omitempty"`
	Target    *CallPersonResponse `json:"target,omitempty"`
	QueueID   string              `json:"queueId,omitempty" example:"q-suporte"`
	QueueName string              `json:"queueName,omitempty" example:"Suporte"`
	Notes     string              `json:"notes,omitempty" example:"Quer cancelar o pedido"`
	Reason    string              `json:"reason,omitempty" example:"ended"`
}

type CallHistoryRecordingResponse struct {
	URL         string `json:"url" example:"https://storage.vozko.com.br/recordings/sip-out-2f1c.wav"`
	DurationSec int    `json:"durationSec" example:"120"`
}

type CallDetailResponse struct {
	CallSummaryResponse
	Handlers  []CallPersonResponse          `json:"handlers"`
	Timeline  []CallTimelineEntryResponse   `json:"timeline"`
	Recording *CallHistoryRecordingResponse `json:"recording,omitempty"`
}

func toPerson(person *callhistory.Person) *CallPersonResponse {
	if person == nil {
		return nil
	}
	return &CallPersonResponse{ID: person.ID, Name: person.Name}
}

func toSummary(summary callhistory.Summary) CallSummaryResponse {
	response := CallSummaryResponse{
		CallID:      summary.CallID,
		Direction:   string(summary.Direction),
		Channel:     string(summary.Channel),
		Outcome:     string(summary.Outcome),
		EndReason:   summary.EndReason,
		StartedAt:   summary.StartedAt,
		AnsweredAt:  summary.AnsweredAt,
		EndedAt:     summary.EndedAt,
		TalkSeconds: summary.TalkSeconds,
		RingSeconds: summary.RingSeconds,
		Contact:     CallContactResponse{Number: summary.Contact.Number, LeadID: summary.Contact.LeadID, Name: summary.Contact.Name, Leads: summary.Contact.Holders},
		PlacedBy:    toPerson(summary.PlacedBy),
		AnsweredBy:  toPerson(summary.AnsweredBy),
		Transfers:   summary.Transfers,
	}
	if summary.Charge != nil {
		response.Charge = &CallChargeResponse{AmountMicros: summary.Charge.Micros, Settled: summary.Charge.Settled}
	}
	return response
}

func toList(page *shared.PaginatedResult[callhistory.Summary]) CallListResponse {
	items := make([]CallSummaryResponse, len(page.Items))
	for i, summary := range page.Items {
		items[i] = toSummary(summary)
	}
	return CallListResponse{Items: items, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages}
}

func toDetail(detail *callhistory.Detail) CallDetailResponse {
	response := CallDetailResponse{
		CallSummaryResponse: toSummary(detail.Summary),
		Handlers:            make([]CallPersonResponse, len(detail.Handlers)),
		Timeline:            make([]CallTimelineEntryResponse, len(detail.Timeline)),
	}
	for i, handler := range detail.Handlers {
		response.Handlers[i] = CallPersonResponse{ID: handler.ID, Name: handler.Name}
	}
	for i, entry := range detail.Timeline {
		response.Timeline[i] = CallTimelineEntryResponse{
			Kind: string(entry.Kind), At: entry.At, Actor: toPerson(entry.Actor), Target: toPerson(entry.Target),
			QueueID: entry.TargetQueueID, QueueName: entry.QueueName, Notes: entry.Notes, Reason: entry.Reason,
		}
	}
	if detail.Recording != nil {
		response.Recording = &CallHistoryRecordingResponse{URL: detail.Recording.URL, DurationSec: detail.Recording.DurationSec}
	}
	return response
}
