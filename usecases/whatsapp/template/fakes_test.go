package template_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type sendMockClientFactory struct {
	client      conversation.WhatsAppClient
	clientErr   error
	wabaID      string
	wabaIDErr   error
	wabaForWABA conversation.WhatsAppClient
}

func (f *sendMockClientFactory) ClientForPhone(string) (conversation.WhatsAppClient, error) {
	return f.client, f.clientErr
}
func (f *sendMockClientFactory) ClientForWABA(string) (conversation.WhatsAppClient, error) {
	return f.wabaForWABA, nil
}
func (f *sendMockClientFactory) WABAIdForPhone(string) (string, error) {
	return f.wabaID, f.wabaIDErr
}

type sendMockTemplateRepo struct {
	tmpl    *template.Template
	findErr error
	created *template.Template
}

func (r *sendMockTemplateRepo) Create(t *template.Template) error {
	r.created = t
	return nil
}
func (r *sendMockTemplateRepo) Update(string, *template.Template) error     { return nil }
func (r *sendMockTemplateRepo) Delete(string) error                         { return nil }
func (r *sendMockTemplateRepo) FindByID(string) (*template.Template, error) { return nil, nil }
func (r *sendMockTemplateRepo) FindByExternalID(string) (*template.Template, error) {
	return nil, nil
}
func (r *sendMockTemplateRepo) FindByExternalIDAndWABA(string, string) (*template.Template, error) {
	return nil, nil
}
func (r *sendMockTemplateRepo) BatchFindByExternalIDs([]string) ([]*template.Template, error) {
	return nil, nil
}
func (r *sendMockTemplateRepo) FindByName(string, string) (*template.Template, error) {
	return nil, nil
}
func (r *sendMockTemplateRepo) FindByNameAndWABA(name, lang, waba string) (*template.Template, error) {
	return r.tmpl, r.findErr
}
func (r *sendMockTemplateRepo) List(template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	return nil, nil
}
func (r *sendMockTemplateRepo) UpdateStatus(string, template.TemplateStatus) error {
	return nil
}
func (r *sendMockTemplateRepo) UpdateHeaderMediaURL(string, *string) error       { return nil }
func (r *sendMockTemplateRepo) UpdateHeaderMedia(string, *string, *string) error { return nil }
func (r *sendMockTemplateRepo) SyncFromExternal(*template.Template) error        { return nil }
