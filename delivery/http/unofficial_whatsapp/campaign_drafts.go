package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"net/http"

	"vozko/delivery/http/campaigndrafthttp"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

type draftPayload struct {
	campaignPayload
	DealPipelineID string `json:"dealPipelineId,omitempty"`
}

type DraftFinisher struct {
	h      *CampaignHandler
	launch *uwcuc.DraftLaunch
	deals  campaigndrafthttp.DealAutomation
}

func NewDraftFinisher(h *CampaignHandler, launch *uwcuc.DraftLaunch, deals campaigndrafthttp.DealAutomation) *DraftFinisher {
	return &DraftFinisher{h: h, launch: launch, deals: deals}
}

func (f *DraftFinisher) decode(w http.ResponseWriter, r *http.Request, raw json.RawMessage) (draftPayload, *uwc.Campaign, uw.DepartmentScope, bool) {
	var payload draftPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "campaign_draft_invalid", "The draft holds an invalid campaign", nil)
		return payload, nil, uw.DepartmentScope{}, false
	}
	payload.ScheduledStart = nil
	workspaceID, scope, ok := f.h.campaignScope(w, r)
	if !ok {
		return payload, nil, uw.DepartmentScope{}, false
	}
	camp, ok := f.h.campaignOf(w, r, workspaceID, payload.campaignPayload)
	return payload, camp, scope, ok
}

func (f *DraftFinisher) Review(w http.ResponseWriter, r *http.Request, raw json.RawMessage) {
	if f.launch == nil {
		response.WriteError(w, http.StatusServiceUnavailable, uwcuc.ErrDraftLaunchUnavailable.Error(), nil)
		return
	}
	_, camp, scope, ok := f.decode(w, r, raw)
	if !ok {
		return
	}
	review, err := f.launch.Review(r.Context(), camp, scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	body := campaigndrafthttp.ReviewResponse{Audience: review.Audience, Check: campaigndrafthttp.CheckOf(review.Report)}
	if review.Estimate != nil {
		body.Estimate = draftEstimateToDTO(review.Estimate)
	}
	response.WriteSuccess(w, http.StatusOK, body)
}

func (f *DraftFinisher) Launch(w http.ResponseWriter, r *http.Request, raw json.RawMessage, launch campaign.Launch, _ string) bool {
	if f.launch == nil {
		response.WriteError(w, http.StatusServiceUnavailable, uwcuc.ErrDraftLaunchUnavailable.Error(), nil)
		return false
	}
	payload, camp, scope, ok := f.decode(w, r, raw)
	if !ok {
		return false
	}
	out, err := f.launch.Launch(r.Context(), uwcuc.DraftLaunchInput{
		Campaign: camp,
		Launch:   launch,
		Scope:    scope,
		Settle: func(_ context.Context, created *uwc.Campaign) []string {
			return campaigndrafthttp.ApplyDealAutomation(r, f.deals, shared.EntryType("unofficial_whatsapp"), created.ID, payload.DealPipelineID)
		},
	})
	if err != nil {
		if !campaigndrafthttp.WriteLaunchRefusal(w, err) {
			writeCampaignError(w, err)
		}
		return false
	}
	campaigndrafthttp.WriteLaunched(w, launch, out, func(c *uwc.Campaign) any { return campaignToDTO(c) }, func(err error) string { return string(uwc.StartRefusalCode(err)) })
	return true
}
