package lead

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	workspace_domain "vozko/domain/workspace"
)

const (
	uuidPattern   = httpx.UUIDPattern
	leadIDPath    = "/{id:" + uuidPattern + "}"
	messageIDPath = "/{messageId:" + uuidPattern + "}"
	addressIDPath = "/{addressId:" + uuidPattern + "}"
)

func RegisterRoutes(
	protected *mux.Router,
	h *LeadHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	ld := workspace_domain.ResourceLeads
	ldRoutes := protected.PathPrefix("/leads").Subrouter()
	ldRoutes.HandleFunc("", ac(ld, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("", ac(ld, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)
	ldRoutes.HandleFunc("/search", ac(ld, workspace_domain.ActionRead, h.GetByNumber)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/sections/summary", ac(ld, workspace_domain.ActionRead, h.SummarySection)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/sections/facets", ac(ld, workspace_domain.ActionRead, h.FacetsSection)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/sections/places", ac(ld, workspace_domain.ActionRead, h.PlacesSection)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/actions/preview", ac(ld, workspace_domain.ActionRead, h.PreviewAction)).Methods(http.MethodPost)
	ldRoutes.HandleFunc("/actions/previews"+previewIDPath, ac(ld, workspace_domain.ActionRead, h.ActionPreview)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/actions", ac(ld, workspace_domain.ActionRead, h.StartAction)).Methods(http.MethodPost)
	ldRoutes.HandleFunc("/actions"+runIDPath, ac(ld, workspace_domain.ActionRead, h.ActionRun)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/actions/audiences"+audienceIDPath, ac(ld, workspace_domain.ActionRead, h.ActionAudience)).Methods(http.MethodGet)
	ldRoutes.HandleFunc("/actions/sends/review", ac(ld, workspace_domain.ActionRead, h.ReviewSend)).Methods(http.MethodPost)
	ldRoutes.HandleFunc("/actions/sends/start", ac(ld, workspace_domain.ActionRead, h.StartSend)).Methods(http.MethodPost)
	ldRoutes.HandleFunc("/actions/sends/cancel", ac(ld, workspace_domain.ActionRead, h.CancelSend)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath, ac(ld, workspace_domain.ActionRead, h.GetByID)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath, ac(ld, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	ldRoutes.HandleFunc(leadIDPath, ac(ld, workspace_domain.ActionUpdate, h.RenameLead)).Methods(http.MethodPatch)
	ldRoutes.HandleFunc(leadIDPath+"/block", ac(ld, workspace_domain.ActionBlock, h.BlockLead)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/owner", ac(ld, workspace_domain.ActionAssign, h.SetOwner)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/opt-out", ac(ld, workspace_domain.ActionUpdate, h.OptOut)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/district", ac(ld, workspace_domain.ActionUpdate, h.SetDistrict)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/addresses"+addressIDPath+"/pin", ac(ld, workspace_domain.ActionUpdate, h.PinLocation)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/location-candidates"+messageIDPath+"/accept", ac(ld, workspace_domain.ActionUpdate, h.AcceptLocation)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/anonymize", ac(ld, workspace_domain.ActionAnonymize, h.Anonymize)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/relatives", ac(ld, workspace_domain.ActionRead, h.ListRelatives)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath+"/relatives", ac(ld, workspace_domain.ActionUpdate, h.AddRelative)).Methods(http.MethodPost)
	ldRoutes.HandleFunc(leadIDPath+"/relations", ac(ld, workspace_domain.ActionUpdate, h.LinkRelation)).Methods(http.MethodPost)
	protected.HandleFunc("/lead-relations"+leadIDPath, ac(ld, workspace_domain.ActionUpdate, h.RemoveRelation)).Methods(http.MethodDelete)
	ldRoutes.HandleFunc(leadIDPath+"/campaigns", ac(ld, workspace_domain.ActionRead, h.GetCampaignHistory)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath+"/summary", ac(ld, workspace_domain.ActionRead, h.GetSummary)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath+"/timeline", ac(ld, workspace_domain.ActionRead, h.Timeline)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath+"/deals", ac(ld, workspace_domain.ActionRead, h.Deals)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath+"/campaigns/{campaignId}/entries", ac(ld, workspace_domain.ActionRead, h.GetEntriesByCampaign)).Methods(http.MethodGet)
	ldRoutes.HandleFunc(leadIDPath+"/campaigns/{campaignId}/analysis", ac(ld, workspace_domain.ActionRead, h.GetAnalysisByCampaign)).Methods(http.MethodGet)
}

func RegisterEntryConversationRoutes(
	protected *mux.Router,
	h *LeadHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	cv := workspace_domain.ResourceConversations
	protected.HandleFunc("/entries/{entryId}/conversation", ac(cv, workspace_domain.ActionRead, h.GetConversationByEntry)).Methods(http.MethodGet)
	protected.HandleFunc("/entries/{entryId}/lead", ac(cv, workspace_domain.ActionRead, h.GetLeadByEntry)).Methods(http.MethodGet)
}
