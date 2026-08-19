// Shared test doubles for the WhatsApp template use cases.
//
// They lived in send_template_message_usecase_test.go until that sender was
// deleted — its billing was conditional on a field being non-empty, and the
// billed sender replaced it. The doubles outlived it because the create and
// sync tests use them too.
package template_usecase

import (
	"context"

	"vozko/domain/balance"
	"vozko/domain/business_metrics"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type sendMockWAClient struct {
	syncTemplatesClientMock
	sendInput  *conversation.SendTemplateMessageInput
	sendOutput *conversation.SendTextMessageOutput
	sendErr    error
}

func (m *sendMockWAClient) SendTemplateMessage(_ context.Context, input conversation.SendTemplateMessageInput) (*conversation.SendTextMessageOutput, error) {
	m.sendInput = &input
	return m.sendOutput, m.sendErr
}

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
}

func (r *sendMockTemplateRepo) Create(*template.Template) error             { return nil }
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

type sendMockBillingUC struct {
	executeWorkspaceID string
	executeRefID       string
	executeCategory    string
	executeCalled      bool
	executeErr         error

	refundWorkspaceID string
	refundRefID       string
	refundCategory    string
	refundCalled      bool
	refundErr         error
}

func (m *sendMockBillingUC) Execute(wsID, refID, cat string) (*balance.Transaction, error) {
	m.executeCalled = true
	m.executeWorkspaceID = wsID
	m.executeRefID = refID
	m.executeCategory = cat
	if m.executeErr != nil {
		return nil, m.executeErr
	}
	return &balance.Transaction{ID: "tx-send-1", Amount: 16_667}, nil
}
func (m *sendMockBillingUC) Refund(wsID, refID, cat string) error {
	m.refundCalled = true
	m.refundWorkspaceID = wsID
	m.refundRefID = refID
	m.refundCategory = cat
	return m.refundErr
}
func (m *sendMockBillingUC) GetTemplateCostMicros(string, string) (int64, error) { return 0, nil }

type sendMockMetricUC struct {
	called bool
	input  *business_metrics.RecordMetricInput
	err    error
}

func (m *sendMockMetricUC) Execute(input business_metrics.RecordMetricInput) error {
	m.called = true
	m.input = &input
	return m.err
}

func utilityTemplate() *template.Template {
	return &template.Template{
		ID:         "tpl-1",
		ExternalID: "ext-1",
		WABAId:     "waba-1",
		Name:       "hello_world",
		Language:   "pt_BR",
		Category:   "UTILITY",
		Status:     "APPROVED",
	}
}

func authenticationTemplate() *template.Template {
	t := utilityTemplate()
	t.Category = "AUTHENTICATION"
	return t
}

func marketingTemplate() *template.Template {
	t := utilityTemplate()
	t.Category = "MARKETING"
	return t
}
