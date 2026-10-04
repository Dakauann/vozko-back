package container

import (
	httpdelivery "vozko/delivery/http"
	metaplatformhttp "vozko/delivery/http/metaplatform"
	mp "vozko/domain/metaplatform"
	metaplatform_repository "vozko/infra/repositories/metaplatform"
	iguc "vozko/usecases/instagram"
	mpuc "vozko/usecases/metaplatform"
)

func (c *Container) metaPlatformService() *mpuc.Service {
	if c.metaPlatform != nil {
		return c.metaPlatform
	}
	svc := mpuc.NewService(metaplatform_repository.NewDeletionRequestRepository(c.db), c.cfg.FrontendBaseURL)
	if c.instagram != nil && c.instagram.Enabled {
		svc.Register(mp.AppInstagram, iguc.NewAppUserHandler(c.instagram.Accounts))
	}
	if c.facebook != nil && c.facebook.Enabled {
		svc.Register(mp.AppMeta, c.facebook.AppUsers)
	}
	svc.Register(mp.AppMeta, c.ads.GrantHealth)
	c.metaPlatform = svc
	return svc
}

func (c *Container) metaChannelRoutes() httpdelivery.MetaChannelRoutes {
	routes := httpdelivery.MetaChannelRoutes{
		Platform: metaplatformhttp.NewHandler(c.metaPlatformService(), map[mp.App][]string{
			mp.AppMeta:      append([]string{c.cfg.MetaAppSecret}, c.cfg.MetaAppSecretsExtra...),
			mp.AppInstagram: {c.cfg.InstagramAppSecret},
		}),
	}
	if c.facebook != nil && c.facebook.Enabled {
		routes.Facebook = c.facebook.Handler
		routes.FacebookWebhook = c.facebook.WebhookHandler
	}
	routes.Ads = c.ads.Handler
	routes.AdsWebhook = c.ads.WebhookHandler
	return routes
}
