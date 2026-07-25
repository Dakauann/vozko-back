package template_usecase

import (
	"context"
	"errors"
	"testing"

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

func TestSendTemplate_ValidationErrors(t *testing.T) {
	uc := NewSendTemplateMessageUseCase(nil, nil, nil, nil)

	tests := []struct {
		name  string
		input template.SendTemplateMessageInput
	}{
		{"empty To", template.SendTemplateMessageInput{TemplateName: "hello", BusinessPhoneID: "phone-1"}},
		{"empty TemplateName", template.SendTemplateMessageInput{To: "5511999", BusinessPhoneID: "phone-1"}},
		{"empty BusinessPhoneID", template.SendTemplateMessageInput{To: "5511999", TemplateName: "hello"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uc.Execute(tt.input)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestSendTemplate_ClientForPhoneError(t *testing.T) {
	factory := &sendMockClientFactory{clientErr: errors.New("phone not found")}
	uc := NewSendTemplateMessageUseCase(factory, nil, nil, nil)

	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello", BusinessPhoneID: "phone-1",
	})
	if err == nil {
		t.Fatal("expected error when ClientForPhone fails")
	}
}

func TestSendTemplate_SuccessNoWorkspace_NoBilling(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-1", ResponseStatus: 200},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{}
	metric := &sendMockMetricUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, metric, billing)
	result, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MessageID != "msg-1" {
		t.Errorf("messageID = %q, want msg-1", result.MessageID)
	}
	if billing.executeCalled {
		t.Error("billing should NOT be called when WorkspaceID is empty")
	}
	if !metric.called {
		t.Error("metric should be recorded on success")
	}
}

func TestSendTemplate_SuccessWithBilling_UtilityDebited(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-2", ResponseStatus: 200},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !billing.executeCalled {
		t.Fatal("billing Execute must be called")
	}
	if billing.executeCategory != "UTILITY" {
		t.Errorf("billing category = %q, want UTILITY", billing.executeCategory)
	}
	if billing.executeWorkspaceID != "ws-1" {
		t.Errorf("billing workspace = %q, want ws-1", billing.executeWorkspaceID)
	}
	if billing.refundCalled {
		t.Error("refund should NOT be called on success")
	}
}

func TestSendTemplate_SuccessWithBilling_AuthenticationDebited(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-auth", ResponseStatus: 200},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: authenticationTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if billing.executeCategory != "AUTHENTICATION" {
		t.Errorf("billing category = %q, want AUTHENTICATION", billing.executeCategory)
	}
}

func TestSendTemplate_SuccessWithBilling_MarketingDebited(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-mkt", ResponseStatus: 200},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: marketingTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if billing.executeCategory != "MARKETING" {
		t.Errorf("billing category = %q, want MARKETING", billing.executeCategory)
	}
}

func TestSendTemplate_SendFailsWithBilling_RefundIssued(t *testing.T) {
	waClient := &sendMockWAClient{
		sendErr:    errors.New("meta API 500"),
		sendOutput: &conversation.SendTextMessageOutput{ResponseStatus: 500},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error when Meta send fails")
	}
	if !billing.executeCalled {
		t.Fatal("billing debit must be called before send")
	}
	if !billing.refundCalled {
		t.Fatal("refund must be issued when send fails")
	}
	if billing.refundCategory != "UTILITY" {
		t.Errorf("refund category = %q, want UTILITY", billing.refundCategory)
	}
}

func TestSendTemplate_BillingDebitFails_NoSend(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "should-not-reach"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{executeErr: errors.New("insufficient balance")}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error when billing fails")
	}
	if waClient.sendInput != nil {
		t.Error("WA API should NOT be called when billing debit fails")
	}
}

func TestSendTemplate_TemplateNotFoundWithWorkspace_FailsClosed(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "should-not-reach"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: nil}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "nonexistent", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error when template not found and billing required")
	}
	if billing.executeCalled {
		t.Error("billing must NOT be called when template is not resolved")
	}
}

func TestSendTemplate_TemplateRepoErrorWithWorkspace_FailsClosed(t *testing.T) {
	waClient := &sendMockWAClient{}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{findErr: errors.New("db error")}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error when template repo fails and billing required")
	}
}

func TestSendTemplate_WABAResolveErrorWithWorkspace_FailsClosed(t *testing.T) {
	waClient := &sendMockWAClient{}
	factory := &sendMockClientFactory{
		client:    waClient,
		wabaIDErr: errors.New("waba not configured"),
	}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error when WABA resolution fails with billing workspace")
	}
}

func TestSendTemplate_EmptyWABAWithWorkspace_FailsClosed(t *testing.T) {
	waClient := &sendMockWAClient{}
	factory := &sendMockClientFactory{
		client: waClient,
		wabaID: "   ",
	}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error when WABA is blank with billing workspace")
	}
}

func TestSendTemplate_InvalidBillingCategory_FailsClosed(t *testing.T) {
	tmpl := utilityTemplate()
	tmpl.Category = "UNKNOWN_CAT"

	waClient := &sendMockWAClient{}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: tmpl}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error for invalid billing category")
	}
	if billing.executeCalled {
		t.Error("billing should NOT be called with invalid category")
	}
}

func TestSendTemplate_DefaultLanguage(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-lang"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, nil)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waClient.sendInput == nil {
		t.Fatal("expected WA send to be called")
	}
	if waClient.sendInput.Language != "pt_BR" {
		t.Errorf("language = %q, want pt_BR", waClient.sendInput.Language)
	}
}

func TestSendTemplate_CustomLanguage(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-en"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, nil)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		Language: "en_US",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waClient.sendInput.Language != "en_US" {
		t.Errorf("language = %q, want en_US", waClient.sendInput.Language)
	}
}

func TestSendTemplate_NilTemplateRepo_SkipsResolution(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-no-repo"},
	}
	factory := &sendMockClientFactory{client: waClient}

	uc := NewSendTemplateMessageUseCase(factory, nil, nil, nil)
	result, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MessageID != "msg-no-repo" {
		t.Errorf("messageID = %q, want msg-no-repo", result.MessageID)
	}
}

func TestSendTemplate_MetricRecordError_DoesNotFail(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-met-err"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	metric := &sendMockMetricUC{err: errors.New("metric db down")}

	uc := NewSendTemplateMessageUseCase(factory, repo, metric, nil)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("metric error should not propagate: %v", err)
	}
}

func TestSendTemplate_NilMetricRecorder_DoesNotPanic(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-nil-met"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, nil)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("should not fail with nil metric recorder: %v", err)
	}
}

func TestSendTemplate_RefundAlsoFails_StillReturnsOriginalError(t *testing.T) {
	waClient := &sendMockWAClient{
		sendErr:    errors.New("meta API 500"),
		sendOutput: &conversation.SendTextMessageOutput{ResponseStatus: 500},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{
		refundErr: errors.New("refund db error"),
	}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	if err.Error() != "meta API 500" {
		t.Errorf("error = %q, want original send error", err.Error())
	}
	if !billing.refundCalled {
		t.Error("refund should still be attempted even if it fails")
	}
}

func TestSendTemplate_SendFailsNoWorkspace_NoRefundAttempted(t *testing.T) {
	waClient := &sendMockWAClient{
		sendErr:    errors.New("meta API error"),
		sendOutput: &conversation.SendTextMessageOutput{ResponseStatus: 400},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	result, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err == nil {
		t.Fatal("expected error from failed send")
	}

	if result == nil {
		t.Fatal("result should not be nil even on send error")
	}
	if result.ResponseStatus != 400 {
		t.Errorf("responseStatus = %d, want 400", result.ResponseStatus)
	}
	if billing.refundCalled {
		t.Error("refund should NOT be attempted without workspace")
	}
}

func imageTemplate(headerMediaID *string) *template.Template {
	t := utilityTemplate()
	t.Components = []template.TemplateComponent{
		{Type: "HEADER", Format: "IMAGE"},
		{Type: "BODY", Text: "Hello {{1}}"},
	}
	t.HeaderMediaID = headerMediaID
	return t
}

func TestSendTemplate_MediaHeaderWithID_SetsHeaderFields(t *testing.T) {
	mediaID := "h:1234"
	tmpl := imageTemplate(&mediaID)

	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-media"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: tmpl}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if waClient.sendInput == nil {
		t.Fatal("expected WA send to be called")
	}
	if waClient.sendInput.HeaderType != "image" {
		t.Errorf("HeaderType = %q, want image", waClient.sendInput.HeaderType)
	}
	if waClient.sendInput.HeaderMediaID != "h:1234" {
		t.Errorf("HeaderMediaID = %q, want h:1234", waClient.sendInput.HeaderMediaID)
	}
}

func TestSendTemplate_MediaHeaderWithoutID_WarningOnly(t *testing.T) {
	tmpl := imageTemplate(nil)

	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-no-media"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: tmpl}
	billing := &sendMockBillingUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, billing)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if waClient.sendInput.HeaderType != "" {
		t.Errorf("HeaderType = %q, want empty", waClient.sendInput.HeaderType)
	}
	if waClient.sendInput.HeaderMediaID != "" {
		t.Errorf("HeaderMediaID = %q, want empty", waClient.sendInput.HeaderMediaID)
	}
}

func TestSendTemplate_HeaderTextParamsNotProvided_WarningOnly(t *testing.T) {
	tmpl := utilityTemplate()

	tmpl.Components = []template.TemplateComponent{
		{Type: "HEADER", Format: "TEXT", Text: "Hi {{name}}"},
		{Type: "BODY", Text: "Welcome"},
	}

	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "msg-hdr-warn"},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: tmpl}

	uc := NewSendTemplateMessageUseCase(factory, repo, nil, nil)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendTemplate_EmptyMessageID_GeneratesUUIDForMetric(t *testing.T) {
	waClient := &sendMockWAClient{
		sendOutput: &conversation.SendTextMessageOutput{MessageID: "", ResponseStatus: 200},
	}
	factory := &sendMockClientFactory{client: waClient, wabaID: "waba-1"}
	repo := &sendMockTemplateRepo{tmpl: utilityTemplate()}
	metric := &sendMockMetricUC{}

	uc := NewSendTemplateMessageUseCase(factory, repo, metric, nil)
	_, err := uc.Execute(template.SendTemplateMessageInput{
		To: "5511999", TemplateName: "hello_world", BusinessPhoneID: "phone-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !metric.called {
		t.Fatal("metric should be recorded")
	}

	if metric.input.EntityID == "" {
		t.Error("entityID should be a generated UUID when MessageID is empty")
	}
	if metric.input.EntityID == "" {
		t.Error("entityID must not be empty")
	}
}

func (m *sendMockWAClient) SendCallPermissionRequest(context.Context, conversation.SendCallPermissionRequestInput) (*conversation.SendTextMessageOutput, error) {
	return &conversation.SendTextMessageOutput{}, nil
}
