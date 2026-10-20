package unofficial_whatsapp

import (
	"math"
	"strings"

	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type campaignEstimateDTO struct {
	Seconds    int64 `json:"seconds"`
	Pending    int   `json:"pending"`
	Senders    int   `json:"senders"`
	Unsendable int   `json:"unsendable"`
}

type campaignSenderDTO struct {
	InstanceID string `json:"instanceId"`
	Label      string `json:"label"`
}

type campaignGroupSendersDTO struct {
	JID     string              `json:"jid"`
	Name    string              `json:"name"`
	Senders []campaignSenderDTO `json:"senders"`
	Block   string              `json:"block,omitempty" enums:"not_in_group,cannot_post,offline"`
}

type campaignEstimateDraftDTO struct {
	Estimate campaignEstimateDTO       `json:"estimate"`
	Groups   []campaignGroupSendersDTO `json:"groups"`
}

type campaignEstimatePayload struct {
	Audience       string   `json:"audience" enums:"leads,groups"`
	InstanceID     string   `json:"instanceId,omitempty"`
	SendDelayMinMS int      `json:"sendDelayMinMs,omitempty"`
	SendDelayMaxMS int      `json:"sendDelayMaxMs,omitempty"`
	Targets        int      `json:"targets,omitempty"`
	GroupJIDs      []string `json:"groupJids,omitempty"`
}

func (p campaignEstimatePayload) toDomain(workspaceID string) uwc.EstimateInput {
	return uwc.EstimateInput{
		WorkspaceID:    workspaceID,
		Audience:       uwc.Audience(strings.TrimSpace(p.Audience)),
		InstanceID:     strings.TrimSpace(p.InstanceID),
		SendDelayMinMS: p.SendDelayMinMS,
		SendDelayMaxMS: p.SendDelayMaxMS,
		Targets:        p.Targets,
		GroupJIDs:      p.GroupJIDs,
	}
}

func estimateToDTO(e uwc.SendEstimate) campaignEstimateDTO {
	return campaignEstimateDTO{
		Seconds:    int64(math.Ceil(e.Duration.Seconds())),
		Pending:    e.Pending,
		Senders:    e.Senders,
		Unsendable: e.Unsendable,
	}
}

func draftEstimateToDTO(d *uwc.DraftEstimate) campaignEstimateDraftDTO {
	out := campaignEstimateDraftDTO{Estimate: estimateToDTO(d.Estimate), Groups: make([]campaignGroupSendersDTO, 0, len(d.Groups))}
	for _, g := range d.Groups {
		senders := make([]campaignSenderDTO, 0, len(g.Senders))
		for _, s := range g.Senders {
			senders = append(senders, campaignSenderDTO{InstanceID: s.InstanceID, Label: s.Label})
		}
		out.Groups = append(out.Groups, campaignGroupSendersDTO{JID: g.JID, Name: g.Name, Senders: senders, Block: string(g.Block)})
	}
	return out
}
