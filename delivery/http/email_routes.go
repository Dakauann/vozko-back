package http

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/emailapihttp"
	emailcampaignhttp "vozko/delivery/http/emailcampaign"
	emailhygienehttp "vozko/delivery/http/emailhygiene"
	emailinboxhttp "vozko/delivery/http/emailinbox"
	emailoutreachhttp "vozko/delivery/http/emailoutreach"
	"vozko/delivery/http/emailpricing"
	emailsendinghttp "vozko/delivery/http/emailsending"
	"vozko/delivery/http/emailwebhooks"
	workspace_domain "vozko/domain/workspace"
)

type EmailRoutes struct {
	Sending      *emailsendinghttp.Handler
	SendingAdmin *emailsendinghttp.AdminHandler
	Webhooks     *emailwebhooks.Handler
	Campaigns    *emailcampaignhttp.Handler
	Consent      *emailcampaignhttp.ConsentHandler
	Public       *emailcampaignhttp.PublicHandler
	Pricing      *emailpricing.Handler
	Inboxes      *emailinboxhttp.Handler
	Outreach     *emailoutreachhttp.Handler
	API          *emailapihttp.Handler
	Hygiene      *emailhygienehttp.Handler
}

func (r *router) setupEmailProtectedRoutes(protected *mux.Router) {
	guard := func(action workspace_domain.Action) emailsendinghttp.Guard {
		return func(f http.HandlerFunc) http.HandlerFunc {
			return r.ac(workspace_domain.ResourceEmailMarketing, action, f)
		}
	}
	emailinboxhttp.RegisterProtectedRoutes(protected, r.email.Inboxes, r.ac)
	emailoutreachhttp.RegisterProtectedRoutes(protected, r.email.Outreach, r.ac)
	emailapihttp.RegisterProtectedRoutes(protected, r.email.API, r.ac)
	emailhygienehttp.RegisterProtectedRoutes(protected, r.email.Hygiene, r.ac)
	emailsendinghttp.RegisterProtectedRoutes(protected, r.email.Sending, emailsendinghttp.Guards{
		View:   guard(workspace_domain.ActionRead),
		Manage: guard(workspace_domain.ActionConfigure),
	})
	emailcampaignhttp.RegisterRoutes(protected, r.email.Campaigns, r.email.Consent, r.ac)
	emailpricing.RegisterRoutes(protected, r.email.Pricing, r.ac)
}

func (r *router) setupEmailAdminRoutes(admin *mux.Router) {
	emailsendinghttp.RegisterAdminRoutes(admin, r.email.SendingAdmin)
}

func (r *router) setupEmailPublicRoutes() {
	emailwebhooks.RegisterPublicRoutes(r.mux, r.email.Webhooks)
	emailcampaignhttp.RegisterPublicRoutes(r.mux, r.email.Public)
}
