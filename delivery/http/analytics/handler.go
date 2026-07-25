package analytics

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"vozko/delivery/http/response"
	analyticsdomain "vozko/domain/analytics"
)

type AnalyticsHandler struct {
	profitReport     analyticsdomain.GetProfitReportUseCase
	callAnalytics    analyticsdomain.GetCallAnalyticsUseCase
	adminOverview    analyticsdomain.GetAdminOverviewUseCase
	planContractions analyticsdomain.GetPlanContractionsUseCase
}

func NewAnalyticsHandler(
	profitReport analyticsdomain.GetProfitReportUseCase,
	callAnalytics analyticsdomain.GetCallAnalyticsUseCase,
	adminOverview analyticsdomain.GetAdminOverviewUseCase,
	planContractions analyticsdomain.GetPlanContractionsUseCase,
) *AnalyticsHandler {
	return &AnalyticsHandler{
		profitReport:     profitReport,
		callAnalytics:    callAnalytics,
		adminOverview:    adminOverview,
		planContractions: planContractions,
	}
}

func (h *AnalyticsHandler) GetAdminOverview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	startDate, endDate, ok := parseDateRange(w, q.Get("startDate"), q.Get("endDate"))
	if !ok {
		return
	}

	input := analyticsdomain.AdminOverviewInput{
		StartDate:          startDate,
		EndDate:            endDate,
		WorkspaceSearch:    strings.TrimSpace(q.Get("search")),
		WorkspacePage:      1,
		WorkspacePageSize:  20,
		WorkspaceSortBy:    q.Get("sortBy"),
		WorkspaceSortOrder: q.Get("sortOrder"),
	}

	if v := q.Get("workspaceId"); v != "" {
		input.WorkspaceID = &v
	}
	if v := q.Get("page"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			input.WorkspacePage = parsed
		}
	}
	if v := q.Get("pageSize"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			input.WorkspacePageSize = parsed
		}
	}
	if v := q.Get("recentLimit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			input.RecentLimit = parsed
		}
	}

	overview, err := h.adminOverview.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to generate admin overview", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, overview)
}

func (h *AnalyticsHandler) GetProfitReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	startDate, endDate, ok := parseDateRange(w, q.Get("startDate"), q.Get("endDate"))
	if !ok {
		return
	}

	var workspaceID *string
	if v := q.Get("workspaceId"); v != "" {
		workspaceID = &v
	}

	granularity := parseGranularity(q.Get("granularity"))

	var serviceTypes []string
	if v := q.Get("serviceType"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				serviceTypes = append(serviceTypes, s)
			}
		}
	}

	topN := 20
	if v := q.Get("topN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			topN = n
		}
	}

	planID, billingCycle, subStatus := parsePlanScope(q)
	input := analyticsdomain.ProfitReportInput{
		WorkspaceID:        workspaceID,
		ServiceTypes:       serviceTypes,
		StartDate:          startDate,
		EndDate:            endDate,
		Granularity:        granularity,
		PlanDefinitionID:   planID,
		BillingCycle:       billingCycle,
		SubscriptionStatus: subStatus,
		TopN:               topN,
	}

	report, err := h.profitReport.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to generate profit report", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, report)
}

func (h *AnalyticsHandler) GetCallAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	startDate, endDate, ok := parseDateRange(w, q.Get("startDate"), q.Get("endDate"))
	if !ok {
		return
	}

	input := analyticsdomain.CallAnalyticsInput{
		StartDate:   startDate,
		EndDate:     endDate,
		Granularity: parseGranularity(q.Get("granularity")),
	}

	if v := q.Get("workspaceId"); v != "" {
		input.WorkspaceID = &v
	}
	if v := q.Get("agentId"); v != "" {
		input.AgentID = &v
	}
	if v := q.Get("campaignId"); v != "" {
		input.CampaignID = &v
	}
	if v := q.Get("callSource"); v != "" {
		input.CallSource = &v
	}
	input.PlanDefinitionID, input.BillingCycle, input.SubscriptionStatus = parsePlanScope(q)

	report, err := h.callAnalytics.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to generate call analytics", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, report)
}

func (h *AnalyticsHandler) GetPlanContractions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	startDate, endDate, ok := parseDateRange(w, q.Get("startDate"), q.Get("endDate"))
	if !ok {
		return
	}

	input := analyticsdomain.PlanContractionsInput{
		StartDate:   startDate,
		EndDate:     endDate,
		Granularity: parseGranularity(q.Get("granularity")),
	}

	if v := q.Get("recentLimit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			input.RecentLimit = parsed
		}
	}

	report, err := h.planContractions.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to generate plan contractions report", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, report)
}

func parseDateRange(w http.ResponseWriter, rawStart, rawEnd string) (start, end time.Time, ok bool) {
	if rawStart == "" || rawEnd == "" {
		response.WriteError(w, http.StatusBadRequest, "startDate and endDate are required (RFC3339)", nil)
		return time.Time{}, time.Time{}, false
	}

	var err error
	start, err = time.Parse(time.RFC3339, rawStart)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid startDate format, expected RFC3339", nil)
		return time.Time{}, time.Time{}, false
	}

	end, err = time.Parse(time.RFC3339, rawEnd)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid endDate format, expected RFC3339", nil)
		return time.Time{}, time.Time{}, false
	}

	if end.Before(start) {
		response.WriteError(w, http.StatusBadRequest, "endDate must be after startDate", nil)
		return time.Time{}, time.Time{}, false
	}

	return start, end, true
}

func parsePlanScope(q url.Values) (planID, billingCycle, status *string) {
	if v := strings.TrimSpace(q.Get("planDefinitionId")); v != "" {
		planID = &v
	}
	if v := strings.TrimSpace(q.Get("billingCycle")); v != "" {
		billingCycle = &v
	}
	if v := strings.TrimSpace(q.Get("subscriptionStatus")); v != "" {
		status = &v
	}
	return planID, billingCycle, status
}

func parseGranularity(v string) analyticsdomain.Granularity {
	switch analyticsdomain.Granularity(v) {
	case analyticsdomain.GranularityHour, analyticsdomain.GranularityDay, analyticsdomain.GranularityWeek,
		analyticsdomain.GranularityMonth, analyticsdomain.GranularityTotal:
		return analyticsdomain.Granularity(v)
	default:
		return analyticsdomain.GranularityDay
	}
}
