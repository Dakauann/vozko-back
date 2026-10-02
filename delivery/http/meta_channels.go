package http

import (
	advertisinghttp "vozko/delivery/http/advertising"
	facebookhttp "vozko/delivery/http/facebook"
	metaplatformhttp "vozko/delivery/http/metaplatform"
	"vozko/delivery/http/metawebhook"
)

type MetaChannelRoutes struct {
	Platform        *metaplatformhttp.Handler
	Facebook        *facebookhttp.Handler
	FacebookWebhook *metawebhook.Handler
	Ads             *advertisinghttp.Handler
	AdsWebhook      *metawebhook.Handler
}
