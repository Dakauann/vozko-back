package buttonaction

import "vozko/domain/whatsapp/template"

func templateWith(body, footer string, buttons ...template.TemplateButton) *template.Template {
	components := []template.TemplateComponent{{Type: "BODY", Text: body}}
	if footer != "" {
		components = append(components, template.TemplateComponent{Type: "FOOTER", Text: footer})
	}
	if len(buttons) > 0 {
		components = append(components, template.TemplateComponent{Type: "BUTTONS", Buttons: buttons})
	}
	return &template.Template{
		ID:         "tmpl-1",
		Name:       "promo_outubro",
		Language:   "pt_BR",
		Category:   template.TemplateCategoryMarketing,
		Status:     template.TemplateStatusApproved,
		Components: components,
	}
}

func quickReply(text string) template.TemplateButton {
	return template.TemplateButton{Type: "QUICK_REPLY", Text: text}
}

func urlButton(text string) template.TemplateButton {
	return template.TemplateButton{Type: "URL", Text: text, URL: "https://loja.exemplo.com"}
}

func promoTemplate() *template.Template {
	return templateWith("Ofertas da semana.", "", quickReply("Falar com atendente"), quickReply("SAIR"), urlButton("Ver loja"))
}
