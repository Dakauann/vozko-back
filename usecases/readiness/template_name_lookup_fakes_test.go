package readiness_usecase

import tmpl "vozko/domain/whatsapp/template"

func (templates) FindByName(string, string, string) ([]*tmpl.Template, error) {
	return nil, tmpl.ErrTemplateLookupUnavailable
}
