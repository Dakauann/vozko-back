package http

import webchathttp "vozko/delivery/http/webchat"

type WebchatRoutes struct {
	Management *webchathttp.Handler
	Public     *webchathttp.PublicHandler
}
