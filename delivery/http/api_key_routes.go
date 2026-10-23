package http

import (
	"vozko/delivery/http/apikeys"
	"vozko/delivery/http/emailapihttp"
)

type APIKeyRoutes struct {
	Handler *apikeys.Handler
	Auth    *apikeys.Authenticator
}

func (r *router) setupKeyRoutes() {
	emailapihttp.RegisterKeyRoutes(r.mux, r.email.API, r.apiKeys.Auth)
}
