package whatsapp_campaign_usecase

import tmpl "vozko/domain/whatsapp/template"

func (t importTemplates) FindByName(string, string, string) ([]*tmpl.Template, error) {
	return nil, tmpl.ErrTemplateLookupUnavailable
}
