package carriermessaging

import (
	"net/http"

	"github.com/gorilla/mux"

	cm "vozko/domain/carriermessaging"
	workspace_domain "vozko/domain/workspace"
)

const LinkPath = "/public/carrier/r/{token}"

type AccessControl func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc

type routeGuards struct {
	read, update, configure, inboxes func(http.HandlerFunc) http.HandlerFunc
}

func guardsOf(ac AccessControl) routeGuards {
	res := workspace_domain.ResourceCarrierMessaging
	return routeGuards{
		read:      func(f http.HandlerFunc) http.HandlerFunc { return ac(res, workspace_domain.ActionRead, f) },
		update:    func(f http.HandlerFunc) http.HandlerFunc { return ac(res, workspace_domain.ActionUpdate, f) },
		configure: func(f http.HandlerFunc) http.HandlerFunc { return ac(res, workspace_domain.ActionConfigure, f) },
		inboxes: func(f http.HandlerFunc) http.HandlerFunc {
			return ac(workspace_domain.ResourceCarrierInboxes, workspace_domain.ActionUpdate, f)
		},
	}
}

func RegisterProtectedRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil || ac == nil {
		return
	}
	g := guardsOf(ac)
	if cm.ChannelRCS.Launched() {
		registerRCSRoutes(protected, h, g)
	}
	if cm.ChannelSMS.Launched() {
		registerSMSRoutes(protected, h, g)
	}

	protected.HandleFunc("/carrier/channels", g.read(h.ListChannels)).Methods(http.MethodGet)
	protected.HandleFunc("/carrier/settings", g.read(h.GetSettings)).Methods(http.MethodGet)
	protected.HandleFunc("/carrier/settings", g.configure(h.PutSettings)).Methods(http.MethodPut)
	protected.HandleFunc("/carrier/suppressions", g.read(h.ListSuppressions)).Methods(http.MethodGet)
	protected.HandleFunc("/carrier/suppressions", g.configure(h.AddSuppression)).Methods(http.MethodPost)
	protected.HandleFunc("/carrier/suppressions/{phone}", g.configure(h.RemoveSuppression)).Methods(http.MethodDelete)
	protected.HandleFunc("/carrier/test-sends", g.update(h.TestSend)).Methods(http.MethodPost)
}

func registerSMSRoutes(protected *mux.Router, h *Handler, g routeGuards) {
	routes := protected.PathPrefix("/sms/routes").Subrouter()
	routes.HandleFunc("", g.read(h.ListRoutes)).Methods(http.MethodGet)
	routes.HandleFunc("", g.configure(h.CreateRoute)).Methods(http.MethodPost)
	routes.HandleFunc("/{id}", g.configure(h.UpdateRoute)).Methods(http.MethodPatch)
	routes.HandleFunc("/{id}", g.configure(h.DeleteRoute)).Methods(http.MethodDelete)
	routes.HandleFunc("/{id}/pause", g.configure(h.PauseRoute)).Methods(http.MethodPost)
	routes.HandleFunc("/{id}/resume", g.configure(h.ResumeRoute)).Methods(http.MethodPost)
	routes.HandleFunc("/{id}/inbox", g.inboxes(h.SetRouteInbox)).Methods(http.MethodPut)

	protected.HandleFunc("/sms/text/check", g.read(h.CheckText)).Methods(http.MethodPost)
	protected.HandleFunc("/sms/text/to-gsm", g.read(h.ToGSM)).Methods(http.MethodPost)
}

func registerRCSRoutes(protected *mux.Router, h *Handler, g routeGuards) {
	senders := protected.PathPrefix("/rcs/senders").Subrouter()
	senders.HandleFunc("", g.read(h.ListSenders)).Methods(http.MethodGet)
	senders.HandleFunc("", g.configure(h.CreateSender)).Methods(http.MethodPost)
	senders.HandleFunc("/{id}", g.read(h.GetSender)).Methods(http.MethodGet)
	senders.HandleFunc("/{id}", g.configure(h.UpdateSender)).Methods(http.MethodPatch)
	senders.HandleFunc("/{id}", g.configure(h.DeleteSender)).Methods(http.MethodDelete)
	senders.HandleFunc("/{id}/submit", g.configure(h.SubmitSender)).Methods(http.MethodPost)
	senders.HandleFunc("/{id}/pause", g.configure(h.PauseSender)).Methods(http.MethodPost)
	senders.HandleFunc("/{id}/resume", g.configure(h.ResumeSender)).Methods(http.MethodPost)
	senders.HandleFunc("/{id}/inbox", g.inboxes(h.SetSenderInbox)).Methods(http.MethodPut)

	protected.HandleFunc("/rcs/content/check", g.read(h.CheckContent)).Methods(http.MethodPost)
	protected.HandleFunc("/rcs/media", g.update(h.ResolveMedia)).Methods(http.MethodPost)
}

func RegisterAdminRoutes(admin *mux.Router, h *Handler) {
	if h == nil {
		return
	}
	if cm.ChannelRCS.Launched() {
		registerRCSAdminRoutes(admin, h)
	}
	if cm.ChannelSMS.Launched() {
		registerSMSAdminRoutes(admin, h)
	}
}

func registerSMSAdminRoutes(admin *mux.Router, h *Handler) {
	admin.HandleFunc("/carrier/sms/routes", h.AdminListRoutes).Methods(http.MethodGet)
	admin.HandleFunc("/carrier/sms/routes/{id}/number", h.AdminAssignNumber).Methods(http.MethodPut)
	admin.HandleFunc("/carrier/sms/routes/{id}/number", h.AdminReleaseNumber).Methods(http.MethodDelete)
}

func registerRCSAdminRoutes(admin *mux.Router, h *Handler) {
	admin.HandleFunc("/carrier/senders", h.AdminListSenders).Methods(http.MethodGet)
	admin.HandleFunc("/carrier/senders/{id}/pause", h.AdminPauseSender).Methods(http.MethodPost)
	admin.HandleFunc("/carrier/senders/{id}/resume", h.AdminResumeSender).Methods(http.MethodPost)
}

func RegisterPublicRoutes(public *mux.Router, h *Handler, webhooks *WebhookHandler) {
	if webhooks != nil {
		public.HandleFunc(WebhookPath, webhooks.Handle).Methods(http.MethodPost)
	}
	if h != nil {
		public.HandleFunc(LinkPath, h.FollowLink).Methods(http.MethodGet)
	}
}
