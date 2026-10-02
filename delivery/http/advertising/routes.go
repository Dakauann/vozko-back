package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/domain/advertising"
	workspace_domain "vozko/domain/workspace"
)

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil {
		return
	}
	res := workspace_domain.ResourceAds
	read, create, update, del := workspace_domain.ActionRead, workspace_domain.ActionCreate, workspace_domain.ActionUpdate, workspace_domain.ActionDelete
	get, post, put, patch, remove := http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete

	protected.HandleFunc(advertising.OAuthStartPath, ac(res, create, h.StartConnect)).Methods(get)

	r := protected.PathPrefix("/ads").Subrouter()
	route := func(method, path string, action workspace_domain.Action, handler http.HandlerFunc) {
		r.HandleFunc(path, ac(res, action, handler)).Methods(method)
	}

	route(get, "/options", read, h.Options)

	route(get, "/accounts", read, h.ListAccounts)
	route(post, "/accounts/{id}/sync", read, h.SyncAccount)
	route(remove, "/accounts/{id}", del, h.DisconnectAccount)
	route(put, "/accounts/{id}/spend-cap", update, h.SetSpendCap)

	route(get, "/accounts/{id}/report", read, h.Report)
	route(get, "/accounts/{id}/report.csv", read, h.ReportCSV)
	route(get, "/accounts/{id}/trend", read, h.Trend)
	route(get, "/accounts/{id}/insights", read, h.Insights)

	route(get, "/accounts/{id}/pages", read, h.Pages)
	route(get, "/accounts/{id}/locations", create, h.Locations)
	route(get, "/accounts/{id}/targeting", create, h.Targeting)
	route(post, "/accounts/{id}/reach-estimate", create, h.Reach)
	route(get, "/accounts/{id}/catalogs", create, h.Catalogs)
	route(get, "/accounts/{id}/apps", create, h.Apps)
	route(get, "/accounts/{id}/pages/{pageId}/posts", create, h.Posts)
	route(get, "/accounts/{id}/pages/{pageId}/instant-experiences", create, h.InstantExperiences)
	route(get, "/accounts/{id}/pixels", read, h.Pixels)
	route(post, "/accounts/{id}/pixels", create, h.CreatePixel)
	route(get, "/accounts/{id}/budget-minimum", read, h.BudgetMinimum)

	route(post, "/objects/{metaId}/activate", workspace_domain.ActionStart, h.Activate)
	route(post, "/objects/{metaId}/pause", workspace_domain.ActionStop, h.Pause)
	route(patch, "/objects/{metaId}/budget", update, h.SetBudget)
	route(get, "/objects/{metaId}", read, h.ObjectDetail)
	route(patch, "/objects/{metaId}", update, h.EditObject)
	route(post, "/objects/{metaId}/copies", create, h.CopyObject)
	route(post, "/objects/{metaId}/archive", update, h.ArchiveObject)
	route(remove, "/objects/{metaId}", del, h.DeleteObject)

	route(get, "/accounts/{id}/tests", read, h.SplitTests)
	route(post, "/tests", create, h.CreateSplitTest)

	route(get, "/accounts/{id}/audiences", read, h.Audiences)
	route(post, "/audiences/customer-list", create, h.CreateCustomerList)
	route(post, "/audiences/lookalike", create, h.CreateLookalike)
	route(remove, "/audiences/{metaId}", del, h.DeleteAudience)
	route(get, "/saved-audiences", read, h.SavedAudiences)
	route(post, "/saved-audiences", create, h.CreateSavedAudience)
	route(put, "/saved-audiences/{id}", update, h.UpdateSavedAudience)
	route(remove, "/saved-audiences/{id}", del, h.DeleteSavedAudience)

	route(get, "/accounts/{id}/pages/{pageId}/forms", read, h.Forms)
	route(post, "/forms", create, h.CreateForm)
	route(post, "/forms/{formId}/archive", update, h.ArchiveForm)
	route(get, "/forms/{formId}/leads", read, h.FormLeads)
	route(post, "/forms/{formId}/sync", read, h.SyncForm)

	route(get, "/accounts/{id}/rules", read, h.Rules)
	route(post, "/rules", create, h.CreateRule)
	route(post, "/rules/{ruleId}/status", update, h.SetRuleStatus)
	route(remove, "/rules/{ruleId}", del, h.DeleteRule)
	route(get, "/rules/{ruleId}/history", read, h.RuleHistory)

	route(get, "/conversions/settings", read, h.ConversionSettings)
	route(put, "/conversions/settings", update, h.SaveConversionSettings)
	route(post, "/conversions/dataset", update, h.ConnectDataset)
	route(get, "/conversions/recent", read, h.RecentConversions)

	route(post, "/drafts/validate", create, h.ValidateDraft)
	route(post, "/publish", create, h.Publish)
	route(get, "/publish-jobs", read, h.ListJobs)
	route(get, "/publish-jobs/{id}", read, h.GetJob)
	route(post, "/publish-jobs/{id}/activate", workspace_domain.ActionStart, h.SwitchOnJob)
	route(get, "/conversations/{entryType}/{entryId}/origin", read, h.ConversationOrigin)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler) {
	if h == nil {
		return
	}
	for _, path := range []string{advertising.OAuthCallbackPath, advertising.OAuthCallbackPath + "/"} {
		public.HandleFunc(path, h.HandleCallback).Methods(http.MethodGet)
	}
}
