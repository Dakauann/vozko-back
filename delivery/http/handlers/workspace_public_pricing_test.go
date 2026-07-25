package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	workspacepricinghttp "vozko/delivery/http/workspacepricing"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type stubListPlansUseCase struct {
	plans []*workspace_plan.PlanDefinition
	err   error
}

func (s stubListPlansUseCase) Execute(includeArchived bool) ([]*workspace_plan.PlanDefinition, error) {
	return s.plans, s.err
}

type stubGetWorkspaceSubscriptionUseCase struct {
	subscription *workspace_plan.WorkspaceSubscriptionDetails
	err          error
}

func (s stubGetWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	return s.subscription, s.err
}

type stubCancelWorkspaceSubscriptionUseCase struct {
	subscription *workspace_plan.WorkspaceSubscriptionDetails
	err          error
}

func (s stubCancelWorkspaceSubscriptionUseCase) Execute(workspaceID string) (*workspace_plan.WorkspaceSubscriptionDetails, error) {
	return s.subscription, s.err
}

type stubGetExchangeRateUseCase struct {
	item *workspace_pricing.PricingItem
	err  error
}

func (s stubGetExchangeRateUseCase) Execute() (*workspace_pricing.PricingItem, error) {
	return s.item, s.err
}

func TestWorkspacePlanHandler_ListPublic_ReturnsPlanDetails(t *testing.T) {
	now := time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC)
	handler := NewWorkspacePlanHandler(
		nil,
		nil,
		nil,
		stubListPlansUseCase{plans: []*workspace_plan.PlanDefinition{{
			ID:                "plan-1",
			Name:              "Growth",
			Description:       "Growth workspace plan",
			BasePriceBRLCents: 15990,
			MaxCallChannels:   4,
			IsGloballyVisible: true,
			CreatedAt:         now,
			UpdatedAt:         now,
		}}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/plans", nil)
	rec := httptest.NewRecorder()

	handler.ListPublic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "\"name\":\"Growth\"") {
		t.Fatalf("expected plan name in response, got %s", body)
	}
	if !strings.Contains(body, "\"basePriceBRLCents\":15990") {
		t.Fatalf("expected basePriceBRLCents in response, got %s", body)
	}
	if !strings.Contains(body, "\"maxCallChannels\":4") {
		t.Fatalf("expected maxCallChannels in response, got %s", body)
	}
}

func TestWorkspacePlanHandler_GetPublicSubscription_ReturnsSubscriptionWithPlan(t *testing.T) {
	now := time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC)
	handler := NewWorkspacePlanHandler(
		nil,
		nil,
		nil,
		nil,
		nil,
		stubGetWorkspaceSubscriptionUseCase{subscription: &workspace_plan.WorkspaceSubscriptionDetails{
			Subscription: &workspace_plan.WorkspaceSubscription{
				ID:                 "sub-1",
				WorkspaceID:        "ws-1",
				PlanDefinitionID:   "plan-1",
				PlanName:           "Starter",
				MaxCallChannels:    3,
				Status:             workspace_plan.SubscriptionStatusActive,
				CurrentPeriodStart: now,
				CurrentPeriodEnd:   now.AddDate(0, 1, 0),
				CreatedAt:          now,
				UpdatedAt:          now,
			},
			Plan: &workspace_plan.PlanDefinition{
				ID:                "plan-1",
				Name:              "Growth",
				BasePriceBRLCents: 15990,
				MaxCallChannels:   4,
			},
		}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/workspaces/ws-1/subscription", nil)
	req = mux.SetURLVars(req, map[string]string{"workspaceId": "ws-1"})
	rec := httptest.NewRecorder()

	handler.GetPublicSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "\"planDefinitionId\":\"plan-1\"") {
		t.Fatalf("expected planDefinitionId in subscription response, got %s", body)
	}
	if !strings.Contains(body, "\"name\":\"Growth\"") {
		t.Fatalf("expected plan name in response, got %s", body)
	}
}

func TestWorkspacePlanHandler_CancelPublicSubscription_ReturnsSubscription(t *testing.T) {
	now := time.Date(2026, time.April, 6, 12, 0, 0, 0, time.UTC)
	handler := NewWorkspacePlanHandler(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		stubCancelWorkspaceSubscriptionUseCase{subscription: &workspace_plan.WorkspaceSubscriptionDetails{
			Subscription: &workspace_plan.WorkspaceSubscription{
				ID:                 "sub-1",
				WorkspaceID:        "ws-1",
				PlanDefinitionID:   "plan-1",
				PlanName:           "Growth",
				MaxCallChannels:    4,
				Status:             workspace_plan.SubscriptionStatusCancelled,
				CurrentPeriodStart: now,
				CurrentPeriodEnd:   now.AddDate(0, 1, 0),
				CancelledAt:        &now,
				CreatedAt:          now,
				UpdatedAt:          now,
			},
			Plan: &workspace_plan.PlanDefinition{
				ID:                "plan-1",
				Name:              "Growth",
				BasePriceBRLCents: 15990,
				MaxCallChannels:   4,
			},
		}},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/workspaces/ws-1/subscription/cancel", nil)
	req = mux.SetURLVars(req, map[string]string{"workspaceId": "ws-1"})
	rec := httptest.NewRecorder()

	handler.Cancel(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "\"status\":\"cancelled\"") {
		t.Fatalf("expected cancelled status in response, got %s", body)
	}
	if !strings.Contains(body, "\"planDefinitionId\":\"plan-1\"") {
		t.Fatalf("expected planDefinitionId in response, got %s", body)
	}
}

func TestWorkspacePricingHandler_GetPublicExchangeRate_HidesInternalPricingFields(t *testing.T) {
	handler := workspacepricinghttp.NewWorkspacePricingHandler(
		nil,
		nil,
		nil,
		nil,
		stubGetExchangeRateUseCase{item: &workspace_pricing.PricingItem{
			ID:          "rate-1",
			Category:    workspace_pricing.CategoryExchangeRate,
			Service:     "usd_to_brl",
			Metric:      "per_unit",
			CostMicros:  123,
			PriceMicros: 6000000,
			MarkupPct:   0.5,
			Currency:    "BRL",
		}},
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/pricing/exchange-rate", nil)
	rec := httptest.NewRecorder()

	handler.GetPublicExchangeRate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "\"priceMicros\"") {
		t.Fatalf("expected public exchange rate to include priceMicros, got %s", body)
	}
	for _, forbidden := range []string{"\"costMicros\"", "\"markupPct\"", "\"category\"", "\"service\"", "\"metric\""} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public exchange rate leaked %s: %s", forbidden, body)
		}
	}
}
