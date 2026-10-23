package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"vozko/delivery/http/campaigndrafthttp"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/buttonaction"
	"vozko/domain/campaign"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
	whatsapp_campaign_usecase "vozko/usecases/whatsapp_campaign"
)

type CampaignRoutesSaver interface {
	CampaignRoutes(ctx context.Context, a buttonaction_usecase.Actor, campaignID string) (*buttonaction_usecase.CampaignView, error)
	SaveCampaignRoutes(ctx context.Context, a buttonaction_usecase.Actor, campaignID string, expectedVersion int64, inputs []buttonaction.Input) (*buttonaction_usecase.CampaignView, error)
}

type whatsappDraftRequest struct {
	whatsappCampaignRequest
	DealPipelineID string                      `json:"dealPipelineId,omitempty"`
	ButtonRoutes   []httpx.ButtonActionRequest `json:"buttonRoutes,omitempty"`
}

type WhatsAppDraftFinisher struct {
	h      *WhatsAppCampaignHandler
	launch *whatsapp_campaign_usecase.DraftLaunch
	deals  campaigndrafthttp.DealAutomation
	routes CampaignRoutesSaver
}

func NewWhatsAppDraftFinisher(h *WhatsAppCampaignHandler, launch *whatsapp_campaign_usecase.DraftLaunch, deals campaigndrafthttp.DealAutomation, routes CampaignRoutesSaver) *WhatsAppDraftFinisher {
	return &WhatsAppDraftFinisher{h: h, launch: launch, deals: deals, routes: routes}
}

type SendQuoteResponse struct {
	Category        string  `json:"category"`
	UnitPriceMicros int64   `json:"unitPriceMicros"`
	Eligible        int64   `json:"eligible"`
	CostMicros      int64   `json:"costMicros"`
	BalanceMicros   int64   `json:"balanceMicros"`
	Currency        string  `json:"currency"`
	Affordable      bool    `json:"affordable"`
	UnitPriceBRL    float64 `json:"unitPriceBrl"`
	CostBRL         float64 `json:"costBrl"`
	BalanceBRL      float64 `json:"balanceBrl"`
}

func quoteResponse(q *template.SendCost, brl *template.SendCostBRL) *SendQuoteResponse {
	if q == nil || brl == nil {
		return nil
	}
	return &SendQuoteResponse{Category: q.Category, UnitPriceMicros: q.UnitPriceMicros, Eligible: q.Eligible, CostMicros: q.CostMicros, BalanceMicros: q.BalanceMicros, Currency: q.Currency, Affordable: q.Affordable,
		UnitPriceBRL: brl.UnitPriceBRL, CostBRL: brl.CostBRL, BalanceBRL: brl.BalanceBRL}
}

func (f *WhatsAppDraftFinisher) decode(w http.ResponseWriter, r *http.Request, raw json.RawMessage) (whatsappDraftRequest, *wc.Campaign, bool) {
	var req whatsappDraftRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "campaign_draft_invalid", "The draft holds an invalid campaign", nil)
		return req, nil, false
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return req, nil, false
	}
	req.ScheduledStart = nil
	camp, err := req.toCampaign(middleware.GetWorkspaceID(r), claims)
	if err != nil {
		f.h.handleDomainError(w, err)
		return req, nil, false
	}
	return req, camp, true
}

func (f *WhatsAppDraftFinisher) Review(w http.ResponseWriter, r *http.Request, raw json.RawMessage) {
	if f.launch == nil {
		response.WriteError(w, http.StatusServiceUnavailable, whatsapp_campaign_usecase.ErrDraftLaunchUnavailable.Error(), nil)
		return
	}
	req, camp, ok := f.decode(w, r, raw)
	if !ok {
		return
	}
	r = withDepartmentCreationScope(r, req.DepartmentID)
	review, err := f.launch.Review(r.Context(), camp)
	if err != nil {
		f.h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaigndrafthttp.ReviewResponse{Audience: review.Audience, Quote: quoteResponse(review.Quote, review.QuoteBRL), Check: campaigndrafthttp.CheckOf(review.Report)})
}

func (f *WhatsAppDraftFinisher) Launch(w http.ResponseWriter, r *http.Request, raw json.RawMessage, launch campaign.Launch, confirmedFingerprint string) bool {
	if f.launch == nil {
		response.WriteError(w, http.StatusServiceUnavailable, whatsapp_campaign_usecase.ErrDraftLaunchUnavailable.Error(), nil)
		return false
	}
	req, camp, ok := f.decode(w, r, raw)
	if !ok {
		return false
	}
	r = withDepartmentCreationScope(r, req.DepartmentID)
	out, err := f.launch.Launch(r.Context(), whatsapp_campaign_usecase.DraftLaunchInput{
		Campaign:             camp,
		Launch:               launch,
		ConfirmedFingerprint: confirmedFingerprint,
		Departments:          middleware.GetDepartmentFilter(r),
		Settle: func(ctx context.Context, created *wc.Campaign) []string {
			f.h.cloneStages(r, req.whatsappCampaignRequest, created)
			warnings := campaigndrafthttp.ApplyDealAutomation(r, f.deals, shared.EntryType("whatsapp"), created.ID, req.DealPipelineID)
			return append(warnings, f.saveRoutes(ctx, r, created.ID, req.ButtonRoutes)...)
		},
	})
	if err != nil {
		if !campaigndrafthttp.WriteLaunchRefusal(w, err) {
			f.h.handleDomainError(w, err)
		}
		return false
	}
	campaigndrafthttp.WriteLaunched(w, launch, out, func(c *wc.Campaign) any { return c }, nil)
	return true
}

func (f *WhatsAppDraftFinisher) saveRoutes(ctx context.Context, r *http.Request, campaignID string, routes []httpx.ButtonActionRequest) []string {
	if len(routes) == 0 {
		return nil
	}
	viewer, ok := httpx.WorkspaceActor(r)
	if f.routes == nil || !ok {
		return []string{campaigndrafthttp.WarningButtonRoutes}
	}
	actor := buttonaction_usecase.Actor{WorkspaceID: viewer.WorkspaceID, UserID: viewer.UserID, IsAdmin: viewer.IsAdmin, Departments: middleware.GetDepartmentFilter(r)}
	current, err := f.routes.CampaignRoutes(ctx, actor, campaignID)
	if err == nil {
		_, err = f.routes.SaveCampaignRoutes(ctx, actor, campaignID, current.Routes.Version, httpx.ButtonActionInputs(routes))
	}
	if err != nil {
		log.Printf("[campaign-drafts] button routes for campaign %s: %v", campaignID, err)
		return []string{campaigndrafthttp.WarningButtonRoutes}
	}
	return nil
}
