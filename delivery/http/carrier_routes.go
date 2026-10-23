package http

import (
	"github.com/gorilla/mux"

	carriercampaignhttp "vozko/delivery/http/carriercampaign"
	carrierhttp "vozko/delivery/http/carriermessaging"
)

type CarrierRoutes struct {
	Messaging *carrierhttp.Handler
	Webhook   *carrierhttp.WebhookHandler
	Campaigns *carriercampaignhttp.Handler
}

func (r *router) setupCarrierProtectedRoutes(protected *mux.Router) {
	carrierhttp.RegisterProtectedRoutes(protected, r.carrier.Messaging, r.ac)
	carriercampaignhttp.RegisterProtectedRoutes(protected, r.carrier.Campaigns, r.ac)
}

func (r *router) setupCarrierAdminRoutes(admin *mux.Router) {
	carrierhttp.RegisterAdminRoutes(admin, r.carrier.Messaging)
}

func (r *router) setupCarrierPublicRoutes() {
	carrierhttp.RegisterPublicRoutes(r.mux, r.carrier.Messaging, r.carrier.Webhook)
}
