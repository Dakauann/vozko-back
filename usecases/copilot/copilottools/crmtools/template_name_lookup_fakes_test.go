package crmtools

import tmpl "vozko/domain/whatsapp/template"

func (f *fakeTemplates) FindByName(string, string, string) ([]*tmpl.Template, error) {
	return nil, tmpl.ErrTemplateLookupUnavailable
}

func (g grantedTemplates) FindByName(string, string, string) ([]*tmpl.Template, error) {
	return nil, tmpl.ErrTemplateLookupUnavailable
}
