package container

import (
	templatehealthhttp "vozko/delivery/http/templatehealthhttp"
	template_usecase "vozko/usecases/whatsapp/template"
)

func (c *Container) templateHealthHandler() *templatehealthhttp.Handler {
	return templatehealthhttp.NewHandler(template_usecase.NewTemplateHealthReader(c.useCases.workspaceTemplates, c.repositories.whatsappQualityTimeline))
}
