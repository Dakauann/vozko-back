package emailcampaignhttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	workspace_domain "vozko/domain/workspace"
)

const (
	campaignsPath = "/email/campaigns"
	campaignPath  = campaignsPath + "/{campaignId:" + httpx.UUIDPattern + "}"
	designsPath   = "/email/designs"
	designPath    = designsPath + "/{designId:" + httpx.UUIDPattern + "}"
	subsPath      = "/email/subscriptions"
)

func RegisterRoutes(protected *mux.Router, h *Handler, consent *ConsentHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc) {
	res := workspace_domain.ResourceEmailMarketing
	read, create, update := workspace_domain.ActionRead, workspace_domain.ActionCreate, workspace_domain.ActionUpdate
	del, start, stop := workspace_domain.ActionDelete, workspace_domain.ActionStart, workspace_domain.ActionStop

	protected.HandleFunc("/email/overview", ac(res, read, h.Overview)).Methods(http.MethodGet)
	protected.HandleFunc("/email/render", ac(res, read, h.Render)).Methods(http.MethodPost)

	protected.HandleFunc(designsPath+"/starters", ac(res, read, h.Starters)).Methods(http.MethodGet)
	protected.HandleFunc(designsPath, ac(res, read, h.ListDesigns)).Methods(http.MethodGet)
	protected.HandleFunc(designsPath, ac(res, create, h.CreateDesign)).Methods(http.MethodPost)
	protected.HandleFunc(designPath, ac(res, read, h.GetDesign)).Methods(http.MethodGet)
	protected.HandleFunc(designPath, ac(res, update, h.UpdateDesign)).Methods(http.MethodPut)
	protected.HandleFunc(designPath, ac(res, del, h.DeleteDesign)).Methods(http.MethodDelete)

	protected.HandleFunc(campaignsPath, ac(res, read, h.ListCampaigns)).Methods(http.MethodGet)
	protected.HandleFunc(campaignsPath, ac(res, create, h.CreateCampaign)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath, ac(res, read, h.GetCampaign)).Methods(http.MethodGet)
	protected.HandleFunc(campaignPath, ac(res, update, h.UpdateCampaign)).Methods(http.MethodPatch)
	protected.HandleFunc(campaignPath, ac(res, del, h.DeleteCampaign)).Methods(http.MethodDelete)
	protected.HandleFunc(campaignPath+"/test", ac(res, update, h.TestSend)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/review", ac(res, update, h.Review)).Methods(http.MethodGet)
	protected.HandleFunc(campaignPath+"/launch", ac(res, start, h.Launch)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/ready", ac(res, start, h.Ready)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/unschedule", ac(res, start, h.Unschedule)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/resume", ac(res, start, h.Resume)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/pause", ac(res, stop, h.Pause)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/stop", ac(res, stop, h.Stop)).Methods(http.MethodPost)
	protected.HandleFunc(campaignPath+"/metrics", ac(res, read, h.Metrics)).Methods(http.MethodGet)
	protected.HandleFunc(campaignPath+"/timeline", ac(res, read, h.Timeline)).Methods(http.MethodGet)
	protected.HandleFunc(campaignPath+"/links", ac(res, read, h.Links)).Methods(http.MethodGet)
	protected.HandleFunc(campaignPath+"/domains", ac(res, read, h.Domains)).Methods(http.MethodGet)
	protected.HandleFunc(campaignPath+"/recipients", ac(res, read, h.Recipients)).Methods(http.MethodGet)

	protected.HandleFunc(subsPath, ac(res, update, consent.Subscribe)).Methods(http.MethodPost)
	protected.HandleFunc(subsPath+"/import", ac(res, update, consent.Import)).Methods(http.MethodPost)
	protected.HandleFunc(subsPath+"/unsubscribe", ac(res, update, consent.UnsubscribeAddress)).Methods(http.MethodPost)
}

func RegisterPublicRoutes(public *mux.Router, h *PublicHandler) {
	p := public.PathPrefix(publicPagePrefix).Subrouter()
	p.HandleFunc("/unsubscribe/{token:[a-z2-7]{20,80}}", h.UnsubscribePage).Methods(http.MethodGet)
	p.HandleFunc("/unsubscribe/{token:[a-z2-7]{20,80}}", h.Unsubscribe).Methods(http.MethodPost)
	p.HandleFunc("/preview", h.Preview).Methods(http.MethodGet)
}
