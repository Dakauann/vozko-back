package http

import tiktokhttp "vozko/delivery/http/tiktok"

type TikTokRoutes struct {
	Handler *tiktokhttp.Handler
	Webhook *tiktokhttp.WebhookHandler
}
