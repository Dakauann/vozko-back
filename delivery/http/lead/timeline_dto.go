package lead

import (
	"time"

	leaddomain "vozko/domain/lead"
	"vozko/domain/opportunity"
)

type LeadTimelineResponse struct {
	LeadID string                 `json:"leadId" example:"6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01"`
	Items  []TimelineItemResponse `json:"items"`
	Next   string                 `json:"next,omitempty" example:"MjAyNi0xMC0wOFQxNDowMzowMFp8Y2FsbDox"`
}

type TimelineItemResponse struct {
	ID        string                  `json:"id" example:"call:3b8c7e1a-5b1d-4c84-9d0a-2f3b8c7e1a01"`
	Kind      string                  `json:"kind" enums:"conversation,campaign_sent,campaign_delivered,campaign_read,campaign_failed,call,deal,deal_event,memory,record" example:"call"`
	At        string                  `json:"at" example:"2026-10-08T14:03:00Z"`
	Actor     string                  `json:"actor,omitempty" example:"ai:7d0a2f3b-8c7e-4c84-9d0a-2f3b8c7e1a01"`
	ActorName string                  `json:"actorName,omitempty" example:"Sara"`
	Ref       TimelineRefResponse     `json:"ref"`
	Summary   TimelineSummaryResponse `json:"summary"`
}

type TimelineRefResponse struct {
	Type      string `json:"type" enums:"entry,call,deal,memory,lead_event" example:"entry"`
	ID        string `json:"id" example:"9d0a2f3b-8c7e-4c84-9d0a-2f3b8c7e1a01"`
	EntryType string `json:"entryType,omitempty" example:"whatsapp"`
}

type TimelineSummaryResponse struct {
	Channel       string   `json:"channel,omitempty" example:"whatsapp"`
	Status        string   `json:"status,omitempty" example:"READ"`
	Title         string   `json:"title,omitempty" example:"Volta às aulas"`
	CampaignID    string   `json:"campaignId,omitempty"`
	Event         string   `json:"event,omitempty" example:"updated"`
	Fields        []string `json:"fields,omitempty"`
	Direction     string   `json:"direction,omitempty" enums:"inbound,outbound"`
	Source        string   `json:"source,omitempty" example:"sip"`
	DurationSec   int      `json:"durationSec,omitempty" example:"42"`
	AnsweredAt    string   `json:"answeredAt,omitempty" example:"2026-10-08T14:03:04Z"`
	ValueCents    int64    `json:"valueCents,omitempty" example:"150000"`
	Currency      string   `json:"currency,omitempty" example:"BRL"`
	PipelineID    string   `json:"pipelineId,omitempty"`
	StageID       string   `json:"stageId,omitempty"`
	Category      string   `json:"category,omitempty" example:"family"`
	Text          string   `json:"text,omitempty" example:"Tem dois filhos na escola"`
	FromStageID   string   `json:"fromStageId,omitempty"`
	StageName     string   `json:"stageName,omitempty" example:"Visita agendada"`
	FromStageName string   `json:"fromStageName,omitempty" example:"Novo contato"`
	Disposition   string   `json:"disposition,omitempty" example:"interessado"`
	CallbackAt    string   `json:"callbackAt,omitempty" example:"2026-10-09T09:00:00Z"`
	CallListID    string   `json:"callListId,omitempty"`
}

type LeadDealsResponse struct {
	LeadID string                     `json:"leadId" example:"6f1c2f9e-5b1d-4c84-9d0a-2f3b8c7e1a01"`
	Deals  []*opportunity.Opportunity `json:"deals"`
	Next   string                     `json:"next,omitempty"`
}

func timelineTime(at time.Time) string {
	return at.UTC().Format(time.RFC3339Nano)
}

func timelineResponse(leadID string, page leaddomain.TimelinePage) LeadTimelineResponse {
	out := LeadTimelineResponse{LeadID: leadID, Items: make([]TimelineItemResponse, 0, len(page.Items)), Next: page.Next}
	for _, item := range page.Items {
		s := item.Summary
		summary := TimelineSummaryResponse{
			Channel: string(s.Channel), Status: s.Status, Title: s.Title, CampaignID: s.CampaignID, Event: s.Event, Fields: s.Fields,
			Direction: s.Direction, Source: s.Source, DurationSec: s.DurationSec, ValueCents: s.ValueCents, Currency: s.Currency,
			PipelineID: s.PipelineID, StageID: s.StageID, Category: s.Category, Text: s.Text, FromStageID: s.FromStageID, StageName: s.StageName,
			FromStageName: s.FromStageName, Disposition: s.Disposition, CallListID: s.CallListID,
		}
		if s.AnsweredAt != nil {
			summary.AnsweredAt = timelineTime(*s.AnsweredAt)
		}
		if s.CallbackAt != nil {
			summary.CallbackAt = timelineTime(*s.CallbackAt)
		}
		out.Items = append(out.Items, TimelineItemResponse{
			ID: item.ID, Kind: string(item.Kind), At: timelineTime(item.At), Actor: item.Actor, ActorName: item.ActorName,
			Ref:     TimelineRefResponse{Type: string(item.Ref.Type), ID: item.Ref.ID, EntryType: string(item.Ref.EntryType)},
			Summary: summary,
		})
	}
	return out
}

func dealsResponse(leadID string, deals []*opportunity.Opportunity, next string) LeadDealsResponse {
	if deals == nil {
		deals = []*opportunity.Opportunity{}
	}
	return LeadDealsResponse{LeadID: leadID, Deals: deals, Next: next}
}
