package http

import mercadolivrehttp "vozko/delivery/http/mercadolivre"

type MercadoLivreRoutes struct {
	Handler       *mercadolivrehttp.Handler
	Notifications *mercadolivrehttp.NotificationHandler
	Catalog       *mercadolivrehttp.CatalogHandler
}
