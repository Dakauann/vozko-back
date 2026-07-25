package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	affiliate_domain "vozko/domain/affiliate"
	invoice_domain "vozko/domain/invoice"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/infra/http/middleware"
)

type affiliateBrandLookup interface {
	GetByID(ctx context.Context, id string) (*affiliate_domain.Affiliate, error)
	GetByCode(ctx context.Context, code string) (*affiliate_domain.Affiliate, error)
	GetReferralByWorkspaceID(ctx context.Context, workspaceID string) (*affiliate_domain.Referral, error)
}

type WorkspacePlanHandler struct {
	createPlan                workspace_plan.CreatePlanDefinitionUseCase
	updatePlan                workspace_plan.UpdatePlanDefinitionUseCase
	archivePlan               workspace_plan.ArchivePlanDefinitionUseCase
	listPlans                 workspace_plan.ListPlanDefinitionsUseCase
	getPlan                   workspace_plan.GetPlanDefinitionUseCase
	getSubscription           workspace_plan.GetWorkspaceSubscriptionUseCase
	createSubscriptionInvoice workspace_plan.CreateSubscriptionInvoiceUseCase
	cancel                    workspace_plan.CancelWorkspaceSubscriptionUseCase
	setVisibility             workspace_plan.SetPlanVisibilityUseCase
	listVisiblePlans          workspace_plan.ListVisiblePlansUseCase
	setExclusiveAffiliate     workspace_plan.SetPlanExclusiveAffiliateUseCase
	listByAffiliateCode       workspace_plan.ListExclusivePlansByAffiliateCodeUseCase
	listMyExclusivePlans      workspace_plan.ListMyExclusivePlansUseCase
	affiliates                affiliateBrandLookup

	referrals workspace_plan.WorkspaceReferralReader
}

type publicPlanDetailsResponse struct {
	// Plan is the customer-safe plan view (see customer_billing_presenters.go): cost/markup stripped.
	Plan customerPlanDetails `json:"plan"`
}

type publicAffiliateBrandResponse struct {
	Code         string `json:"code"`
	BrandName    string `json:"brandName"`
	BrandLogoURL string `json:"brandLogoUrl"`
}

type publicPlanListingResponse struct {
	Plans             []publicPlanDetailsResponse   `json:"plans"`
	AnnualDiscountPct int                           `json:"annualDiscountPct"`
	AffiliateBrand    *publicAffiliateBrandResponse `json:"affiliateBrand,omitempty"`
}

type publicSubscriptionDetailsResponse struct {
	Subscription *workspace_plan.WorkspaceSubscription `json:"subscription"`
	Plan         *workspace_plan.PlanDefinition        `json:"plan,omitempty"`
}

func NewWorkspacePlanHandler(
	createPlan workspace_plan.CreatePlanDefinitionUseCase,
	updatePlan workspace_plan.UpdatePlanDefinitionUseCase,
	archivePlan workspace_plan.ArchivePlanDefinitionUseCase,
	listPlans workspace_plan.ListPlanDefinitionsUseCase,
	getPlan workspace_plan.GetPlanDefinitionUseCase,
	getSubscription workspace_plan.GetWorkspaceSubscriptionUseCase,
	createSubscriptionInvoice workspace_plan.CreateSubscriptionInvoiceUseCase,
	cancel workspace_plan.CancelWorkspaceSubscriptionUseCase,
	setVisibility workspace_plan.SetPlanVisibilityUseCase,
	listVisiblePlans workspace_plan.ListVisiblePlansUseCase,
	setExclusiveAffiliate workspace_plan.SetPlanExclusiveAffiliateUseCase,
	listByAffiliateCode workspace_plan.ListExclusivePlansByAffiliateCodeUseCase,
	listMyExclusivePlans workspace_plan.ListMyExclusivePlansUseCase,
	affiliates affiliateBrandLookup,
	referrals workspace_plan.WorkspaceReferralReader,
) *WorkspacePlanHandler {
	return &WorkspacePlanHandler{
		createPlan:                createPlan,
		updatePlan:                updatePlan,
		archivePlan:               archivePlan,
		listPlans:                 listPlans,
		getPlan:                   getPlan,
		getSubscription:           getSubscription,
		createSubscriptionInvoice: createSubscriptionInvoice,
		cancel:                    cancel,
		setVisibility:             setVisibility,
		listVisiblePlans:          listVisiblePlans,
		setExclusiveAffiliate:     setExclusiveAffiliate,
		listByAffiliateCode:       listByAffiliateCode,
		listMyExclusivePlans:      listMyExclusivePlans,
		affiliates:                affiliates,
		referrals:                 referrals,
	}
}

func (h *WorkspacePlanHandler) ListPublic(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspaceId")
	refCode := r.URL.Query().Get("ref")

	var plans []*workspace_plan.PlanDefinition
	var err error

	if workspaceID != "" {

		plans, err = h.listVisiblePlans.Execute(workspaceID)
	} else {
		plans, err = h.listPlans.Execute(false)
	}
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}

	result := make([]publicPlanDetailsResponse, 0, len(plans))
	seen := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if workspaceID == "" && !plan.IsGloballyVisible {
			continue
		}
		if _, dup := seen[plan.ID]; dup {
			continue
		}
		seen[plan.ID] = struct{}{}
		result = append(result, publicPlanDetailsResponse{Plan: toCustomerPlanDetails(plan)})
	}

	if workspaceID == "" && refCode != "" && h.listByAffiliateCode != nil {
		exclusive, err := h.listByAffiliateCode.Execute(refCode)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
			return
		}
		for _, plan := range exclusive {
			if _, dup := seen[plan.ID]; dup {
				continue
			}
			seen[plan.ID] = struct{}{}
			result = append(result, publicPlanDetailsResponse{Plan: toCustomerPlanDetails(plan)})
		}
	}

	response.WriteSuccess(w, http.StatusOK, publicPlanListingResponse{
		Plans:             result,
		AnnualDiscountPct: workspace_plan.AnnualDiscountPct,
		AffiliateBrand:    h.resolvePublicAffiliateBrand(r.Context(), workspaceID, refCode),
	})
}

func (h *WorkspacePlanHandler) resolvePublicAffiliateBrand(ctx context.Context, workspaceID, refCode string) *publicAffiliateBrandResponse {
	if h.affiliates == nil {
		return nil
	}
	if workspaceID != "" {

		var affiliateID string
		if h.referrals != nil {
			id, err := h.referrals.GetAffiliateIDByWorkspaceID(workspaceID)
			if err != nil {
				return nil
			}
			affiliateID = id
		}
		if affiliateID == "" {
			ref, err := h.affiliates.GetReferralByWorkspaceID(ctx, workspaceID)
			if err != nil || ref == nil {
				return nil
			}
			affiliateID = ref.AffiliateID
		}
		if affiliateID == "" {
			return nil
		}
		aff, err := h.affiliates.GetByID(ctx, affiliateID)
		if err != nil || aff == nil || !aff.Active {
			return nil
		}
		return &publicAffiliateBrandResponse{Code: aff.Code, BrandName: aff.BrandName, BrandLogoURL: aff.BrandLogoURL}
	}
	if refCode != "" {
		aff, err := h.affiliates.GetByCode(ctx, refCode)
		if err != nil || aff == nil || !aff.Active {
			return nil
		}
		return &publicAffiliateBrandResponse{Code: aff.Code, BrandName: aff.BrandName, BrandLogoURL: aff.BrandLogoURL}
	}
	return nil
}

func (h *WorkspacePlanHandler) ListAdmin(w http.ResponseWriter, r *http.Request) {
	includeArchived := false
	if raw := r.URL.Query().Get("includeArchived"); raw != "" {
		parsed, _ := strconv.ParseBool(raw)
		includeArchived = parsed
	}
	plans, err := h.listPlans.Execute(includeArchived)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, plans)
}

func (h *WorkspacePlanHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	planID := mux.Vars(r)["planId"]
	plan, err := h.getPlan.Execute(planID)
	if err != nil {
		if errors.Is(err, workspace_plan.ErrPlanNotFound) {
			response.WriteError(w, http.StatusNotFound, "Plan not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, plan)
}

func (h *WorkspacePlanHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var input workspace_plan.PlanMutationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	plan, err := h.createPlan.Execute(input)
	if err != nil {
		h.writePlanError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, plan)
}

func (h *WorkspacePlanHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	planID := mux.Vars(r)["planId"]
	var input workspace_plan.PlanMutationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	plan, err := h.updatePlan.Execute(planID, input)
	if err != nil {
		h.writePlanError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, plan)
}

func (h *WorkspacePlanHandler) ArchivePlan(w http.ResponseWriter, r *http.Request) {
	planID := mux.Vars(r)["planId"]
	if err := h.archivePlan.Execute(planID); err != nil {
		if errors.Is(err, workspace_plan.ErrPlanNotFound) {
			response.WriteError(w, http.StatusNotFound, "Plan not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "archived"})
}

func (h *WorkspacePlanHandler) GetSubscription(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		workspaceID = middleware.GetWorkspaceID(r)
	}
	subscription, err := h.getSubscription.Execute(workspaceID)
	if err != nil {
		if errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
			response.WriteError(w, http.StatusNotFound, "Workspace subscription not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, subscription)
}

func (h *WorkspacePlanHandler) GetPublicSubscription(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		workspaceID = middleware.GetWorkspaceID(r)
	}
	subscription, err := h.getSubscription.Execute(workspaceID)
	if err != nil {
		if errors.Is(err, workspace_plan.ErrSubscriptionNotFound) {
			response.WriteError(w, http.StatusNotFound, "Workspace subscription not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, &publicSubscriptionDetailsResponse{
		Subscription: subscription.Subscription,
		Plan:         subscription.Plan,
	})
}

func (h *WorkspacePlanHandler) CreateSubscriptionInvoice(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	workspaceID := mux.Vars(r)["workspaceId"]
	var body workspace_plan.CreateSubscriptionInvoiceInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if body.ReferralCode == "" {
		body.ReferralCode = middleware.ExtractReferralCode(r)
	}
	result, err := h.createSubscriptionInvoice.Execute(workspaceID, claims.UserID, body)
	if err != nil {
		h.writeSubscriptionInvoiceError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, map[string]interface{}{"invoice": result.Invoice})
}

func (h *WorkspacePlanHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	subscription, err := h.cancel.Execute(workspaceID)
	if err != nil {
		h.writeSubscriptionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, subscription)
}

func (h *WorkspacePlanHandler) SetVisibility(w http.ResponseWriter, r *http.Request) {
	planID := mux.Vars(r)["planId"]
	var input workspace_plan.SetPlanVisibilityInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	plan, err := h.setVisibility.Execute(planID, input)
	if err != nil {
		h.writePlanError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, plan)
}

func (h *WorkspacePlanHandler) SetExclusiveAffiliate(w http.ResponseWriter, r *http.Request) {
	planID := mux.Vars(r)["planId"]
	var input workspace_plan.SetPlanExclusiveAffiliateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	plan, err := h.setExclusiveAffiliate.Execute(planID, input)
	if err != nil {
		h.writePlanError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, plan)
}

func (h *WorkspacePlanHandler) ListByAffiliateCode(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	plans, err := h.listByAffiliateCode.Execute(code)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	result := make([]publicPlanDetailsResponse, 0, len(plans))
	for _, plan := range plans {
		result = append(result, publicPlanDetailsResponse{Plan: toCustomerPlanDetails(plan)})
	}
	response.WriteSuccess(w, http.StatusOK, publicPlanListingResponse{
		Plans:             result,
		AnnualDiscountPct: workspace_plan.AnnualDiscountPct,
	})
}

func (h *WorkspacePlanHandler) ListMyExclusivePlans(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil || claims.UserID == "" {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	plans, err := h.listMyExclusivePlans.Execute(claims.UserID)
	if err != nil {
		if errors.Is(err, workspace_plan.ErrAffiliateRequired) {
			response.WriteError(w, http.StatusForbidden, "caller is not a registered affiliate", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	result := make([]publicPlanDetailsResponse, 0, len(plans))
	for _, plan := range plans {
		result = append(result, publicPlanDetailsResponse{Plan: toCustomerPlanDetails(plan)})
	}
	response.WriteSuccess(w, http.StatusOK, publicPlanListingResponse{
		Plans:             result,
		AnnualDiscountPct: workspace_plan.AnnualDiscountPct,
	})
}

func (h *WorkspacePlanHandler) writePlanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace_plan.ErrPlanNotFound):
		response.WriteError(w, http.StatusNotFound, "Plan not found", nil)
	case errors.Is(err, workspace_plan.ErrPlanArchived),
		errors.Is(err, workspace_plan.ErrPlanExclusiveLocked):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrInvalidPlanName),
		errors.Is(err, workspace_plan.ErrInvalidBasePrice),
		errors.Is(err, workspace_plan.ErrInvalidMaxCallChannels),
		errors.Is(err, workspace_plan.ErrExclusiveAffiliateRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func (h *WorkspacePlanHandler) writeSubscriptionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace_plan.ErrPlanNotFound), errors.Is(err, workspace_plan.ErrSubscriptionNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists),
		errors.Is(err, workspace_plan.ErrSubscriptionCancelled),
		errors.Is(err, workspace_plan.ErrPlanArchived):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrDowngradeNotAllowed):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrSubscriptionNotCurrent),
		errors.Is(err, workspace_plan.ErrInvalidSubscription):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func (h *WorkspacePlanHandler) writeSubscriptionInvoiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invoice_domain.ErrCustomerDocumentRequired):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "customer_document_required", err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrPlanNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrSubscriptionAlreadyExists), errors.Is(err, workspace_plan.ErrPlanArchived),
		errors.Is(err, workspace_plan.ErrDowngradeNotAllowed):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, workspace_plan.ErrPlanNotVisible):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, invoice_domain.ErrInvalidAmount),
		errors.Is(err, invoice_domain.ErrInvalidPurpose),
		errors.Is(err, invoice_domain.ErrPlanDefinitionRequired),
		errors.Is(err, workspace_plan.ErrInvalidSubscription):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
