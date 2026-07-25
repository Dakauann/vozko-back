package template_usecase

import (
	"errors"
	"testing"

	"vozko/domain/shared"
	"vozko/domain/whatsapp/template"
)

type mockTemplateRepo struct {
	templates map[string]*template.Template
	createErr error
	updateErr error
	deleteErr error
	findErr   error
	listErr   error
}

func newMockTemplateRepo() *mockTemplateRepo {
	return &mockTemplateRepo{
		templates: make(map[string]*template.Template),
	}
}

func (m *mockTemplateRepo) Create(t *template.Template) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.templates[t.ID] = t
	return nil
}

func (m *mockTemplateRepo) Update(templateID string, t *template.Template) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.templates[templateID] = t
	return nil
}

func (m *mockTemplateRepo) Delete(templateID string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.templates[templateID]; !ok {
		return template.ErrTemplateNotFound
	}
	delete(m.templates, templateID)
	return nil
}

func (m *mockTemplateRepo) FindByID(templateID string) (*template.Template, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	t, ok := m.templates[templateID]
	if !ok {
		return nil, template.ErrTemplateNotFound
	}
	return t, nil
}

func (m *mockTemplateRepo) FindByExternalID(externalID string) (*template.Template, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, t := range m.templates {
		if t.ExternalID == externalID {
			return t, nil
		}
	}
	return nil, template.ErrTemplateNotFound
}

func (m *mockTemplateRepo) FindByExternalIDAndWABA(externalID string, wabaID string) (*template.Template, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, t := range m.templates {
		if t.ExternalID == externalID && t.WABAId == wabaID {
			return t, nil
		}
	}
	return nil, template.ErrTemplateNotFound
}

func (m *mockTemplateRepo) BatchFindByExternalIDs(externalIDs []string) ([]*template.Template, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	idSet := make(map[string]bool, len(externalIDs))
	for _, id := range externalIDs {
		idSet[id] = true
	}
	var result []*template.Template
	for _, t := range m.templates {
		if idSet[t.ExternalID] {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *mockTemplateRepo) FindByName(name, language string) (*template.Template, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, t := range m.templates {
		if t.Name == name && t.Language == language {
			return t, nil
		}
	}
	return nil, template.ErrTemplateNotFound
}

func (m *mockTemplateRepo) FindByNameAndWABA(name, language, wabaID string) (*template.Template, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	for _, t := range m.templates {
		if t.Name == name && t.Language == language && t.WABAId == wabaID {
			return t, nil
		}
	}
	return nil, template.ErrTemplateNotFound
}

func (m *mockTemplateRepo) List(input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var items []*template.Template
	for _, t := range m.templates {
		items = append(items, t)
	}
	return &shared.PaginatedResult[*template.Template]{Items: items, TotalItems: int64(len(items)), Page: 1, PageSize: 100}, nil
}

func (m *mockTemplateRepo) UpdateStatus(templateID string, status template.TemplateStatus) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	t, ok := m.templates[templateID]
	if !ok {
		return template.ErrTemplateNotFound
	}
	t.Status = status
	return nil
}

func (m *mockTemplateRepo) UpdateHeaderMediaURL(templateID string, headerMediaURL *string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	t, ok := m.templates[templateID]
	if !ok {
		return template.ErrTemplateNotFound
	}
	t.HeaderMediaURL = headerMediaURL
	return nil
}

func (m *mockTemplateRepo) UpdateHeaderMedia(templateID string, headerMediaURL *string, headerMediaID *string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	t, ok := m.templates[templateID]
	if !ok {
		return template.ErrTemplateNotFound
	}
	t.HeaderMediaURL = headerMediaURL
	t.HeaderMediaID = headerMediaID
	return nil
}

func (m *mockTemplateRepo) SyncFromExternal(t *template.Template) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.templates[t.ID] = t
	return nil
}

func seedTemplate(repo *mockTemplateRepo, id, externalID, wabaID, name string, status template.TemplateStatus, category template.TemplateCategory) *template.Template {
	t := &template.Template{
		ID:         id,
		ExternalID: externalID,
		WABAId:     wabaID,
		Name:       name,
		Language:   "en_US",
		Status:     status,
		Category:   category,
	}
	repo.templates[id] = t
	return t
}

func makeStatusPayload(wabaID string, templateID int64, name, event, reason, category string) *template.TemplateWebhookPayload {
	return &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: wabaID,
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateStatusUpdate,
				Value: template.TemplateWebhookValue{
					Event:                   event,
					MessageTemplateID:       templateID,
					MessageTemplateName:     name,
					MessageTemplateLanguage: "en_US",
					Reason:                  reason,
					MessageTemplateCategory: category,
				},
			}},
		}},
	}
}

func TestStatusUpdate_Approved(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1689556908129832", "waba1", "order_confirmation", template.TemplateStatusPending, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 1689556908129832, "order_confirmation", "APPROVED", "NONE", "UTILITY")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	tmpl := repo.templates["t1"]
	if tmpl.Status != template.TemplateStatusApproved {
		t.Errorf("status = %s, want APPROVED", tmpl.Status)
	}
}

func TestStatusUpdate_Rejected(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1689556908129835", "waba1", "abandoned_cart", template.TemplateStatusPending, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 1689556908129835, "abandoned_cart", "REJECTED", "INVALID_FORMAT", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	tmpl := repo.templates["t1"]
	if tmpl.Status != template.TemplateStatusRejected {
		t.Errorf("status = %s, want REJECTED", tmpl.Status)
	}
}

func TestStatusUpdate_Paused(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "100", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 100, "promo", "PAUSED", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusPaused {
		t.Errorf("status = %s, want PAUSED", repo.templates["t1"].Status)
	}
}

func TestStatusUpdate_Disabled(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "200", "waba1", "promo", template.TemplateStatusPaused, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 200, "promo", "DISABLED", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusDisabled {
		t.Errorf("status = %s, want DISABLED", repo.templates["t1"].Status)
	}
}

func TestStatusUpdate_Reinstated(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "300", "waba1", "promo", template.TemplateStatusDisabled, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 300, "promo", "REINSTATED", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusApproved {
		t.Errorf("status = %s, want APPROVED (reinstated)", repo.templates["t1"].Status)
	}
}

func TestStatusUpdate_Pending(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "400", "waba1", "auth_otp", template.TemplateStatusApproved, template.TemplateCategoryAuthentication)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 400, "auth_otp", "PENDING", "NONE", "AUTHENTICATION")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusPending {
		t.Errorf("status = %s, want PENDING", repo.templates["t1"].Status)
	}
}

func TestStatusUpdate_InAppeal(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "500", "waba1", "promo", template.TemplateStatusRejected, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 500, "promo", "IN_APPEAL", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusPending {
		t.Errorf("status = %s, want PENDING (in_appeal)", repo.templates["t1"].Status)
	}
}

func TestStatusUpdate_Locked(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "600", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 600, "promo", "LOCKED", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusPaused {
		t.Errorf("status = %s, want PAUSED (locked)", repo.templates["t1"].Status)
	}
}

func TestStatusUpdate_Deleted(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "700", "waba1", "old_template", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 700, "old_template", "DELETED", "", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, ok := repo.templates["t1"]; ok {
		t.Error("template should have been deleted from repo")
	}
}

func TestStatusUpdate_PendingDeletion(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "800", "waba1", "soon_deleted", template.TemplateStatusApproved, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 800, "soon_deleted", "PENDING_DELETION", "", "UTILITY")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, ok := repo.templates["t1"]; ok {
		t.Error("template should have been deleted from repo")
	}
}

func TestStatusUpdate_CategoryUpdatedAlongWithStatus(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "900", "waba1", "promo", template.TemplateStatusPending, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 900, "promo", "APPROVED", "NONE", "UTILITY")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	tmpl := repo.templates["t1"]
	if tmpl.Status != template.TemplateStatusApproved {
		t.Errorf("status = %s, want APPROVED", tmpl.Status)
	}
	if tmpl.Category != template.TemplateCategoryUtility {
		t.Errorf("category = %s, want UTILITY", tmpl.Category)
	}
}

func TestStatusUpdate_TemplateNotFound(t *testing.T) {
	repo := newMockTemplateRepo()
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 999, "nonexistent", "APPROVED", "NONE", "UTILITY")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestStatusUpdate_NoChange(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1000", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 1000, "promo", "APPROVED", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

}

func TestStatusUpdate_RepoUpdateError(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1100", "waba1", "promo", template.TemplateStatusPending, template.TemplateCategoryMarketing)
	repo.updateErr = errors.New("db unavailable")
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 1100, "promo", "APPROVED", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute should not return error: %v", err)
	}
}

func TestStatusUpdate_MissingTemplateID(t *testing.T) {
	repo := newMockTemplateRepo()
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateStatusUpdate,
				Value: template.TemplateWebhookValue{
					Event:               "APPROVED",
					MessageTemplateID:   0,
					MessageTemplateName: "test",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestStatusUpdate_UnrecognisedEvent(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1200", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 1200, "promo", "SOME_FUTURE_EVENT", "NONE", "MARKETING")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusApproved {
		t.Errorf("status should remain APPROVED, got %s", repo.templates["t1"].Status)
	}
}

func TestComponentsUpdate_Logged(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "550", "waba1", "order_update", template.TemplateStatusApproved, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateComponentsUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID:       550,
					MessageTemplateName:     "order_update",
					MessageTemplateLanguage: "en_US",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusApproved {
		t.Errorf("status = %s, want APPROVED (components_update is informational)", repo.templates["t1"].Status)
	}
}

func TestComponentsUpdate_MissingID(t *testing.T) {
	repo := newMockTemplateRepo()
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateComponentsUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID: 0,
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestQualityUpdate_Logged(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "500", "waba1", "welcome_template", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateQualityUpdate,
				Value: template.TemplateWebhookValue{
					PreviousQualityScore:    "GREEN",
					NewQualityScore:         "YELLOW",
					MessageTemplateID:       500,
					MessageTemplateName:     "welcome_template",
					MessageTemplateLanguage: "en_US",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusApproved {
		t.Errorf("status = %s, want APPROVED (quality updates are informational)", repo.templates["t1"].Status)
	}
}

func TestCategoryUpdate_MarketingToUtility(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "600", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldTemplateCategoryUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID:       600,
					MessageTemplateName:     "promo",
					MessageTemplateLanguage: "en_US",
					PreviousCategory:        "MARKETING",
					NewCategory:             "UTILITY",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Category != template.TemplateCategoryUtility {
		t.Errorf("category = %s, want UTILITY", repo.templates["t1"].Category)
	}
}

func TestCategoryUpdate_UtilityToAuthentication(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "601", "waba1", "otp", template.TemplateStatusApproved, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldTemplateCategoryUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID:       601,
					MessageTemplateName:     "otp",
					MessageTemplateLanguage: "en_US",
					PreviousCategory:        "UTILITY",
					NewCategory:             "AUTHENTICATION",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Category != template.TemplateCategoryAuthentication {
		t.Errorf("category = %s, want AUTHENTICATION", repo.templates["t1"].Category)
	}
}

func TestCategoryUpdate_NoChange(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "602", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldTemplateCategoryUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID:   602,
					MessageTemplateName: "promo",
					PreviousCategory:    "MARKETING",
					NewCategory:         "MARKETING",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestCategoryUpdate_InvalidCategory(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "603", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldTemplateCategoryUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID:   603,
					MessageTemplateName: "promo",
					NewCategory:         "INVALID_CATEGORY",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Category != template.TemplateCategoryMarketing {
		t.Errorf("category = %s, want MARKETING (unchanged)", repo.templates["t1"].Category)
	}
}

func TestCategoryUpdate_MissingNewCategory(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "604", "waba1", "promo", template.TemplateStatusApproved, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldTemplateCategoryUpdate,
				Value: template.TemplateWebhookValue{
					MessageTemplateID:   604,
					MessageTemplateName: "promo",
					NewCategory:         "",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestExecuteTemplate_NilPayload(t *testing.T) {
	repo := newMockTemplateRepo()
	uc := NewHandleTemplateWebhook(repo)

	if err := uc.Execute(nil); err != nil {
		t.Fatalf("Execute(nil): %v", err)
	}
}

func TestExecuteTemplate_EmptyEntries(t *testing.T) {
	repo := newMockTemplateRepo()
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry:  []template.TemplateWebhookEntry{},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestStatusUpdate_FallbackToExternalIDOnly(t *testing.T) {
	repo := newMockTemplateRepo()

	seedTemplate(repo, "t1", "1300", "", "legacy_template", template.TemplateStatusPending, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo)

	payload := makeStatusPayload("waba1", 1300, "legacy_template", "APPROVED", "NONE", "UTILITY")

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusApproved {
		t.Errorf("status = %s, want APPROVED (legacy fallback)", repo.templates["t1"].Status)
	}
}

// 360dialog delivers status changes keyed by the channel-scoped template id
// (stored as ExternalID), not Meta's numeric id. The status update must resolve
// the record via ChannelExternalID when MessageTemplateID is absent.
func TestStatusUpdate_Dialog360ChannelExternalID(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "gqh7gkLf8nZPcY4EpEbmWT", "2044166682973889", "concluirinscricao_03", template.TemplateStatusPending, template.TemplateCategoryUtility)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "2044166682973889",
			Changes: []template.TemplateWebhookChange{{
				Field: template.FieldMessageTemplateStatusUpdate,
				Value: template.TemplateWebhookValue{
					Event:               "REJECTED",
					ChannelExternalID:   "gqh7gkLf8nZPcY4EpEbmWT",
					MessageTemplateName: "concluirinscricao_03",
				},
			}},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.templates["t1"].Status != template.TemplateStatusRejected {
		t.Errorf("status = %s, want REJECTED (matched by channel external id)", repo.templates["t1"].Status)
	}
}

func TestMultipleTemplateChangesInSinglePayload(t *testing.T) {
	repo := newMockTemplateRepo()
	seedTemplate(repo, "t1", "1400", "waba1", "promo_a", template.TemplateStatusPending, template.TemplateCategoryMarketing)
	seedTemplate(repo, "t2", "1401", "waba1", "promo_b", template.TemplateStatusPending, template.TemplateCategoryMarketing)
	uc := NewHandleTemplateWebhook(repo)

	payload := &template.TemplateWebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []template.TemplateWebhookEntry{{
			ID: "waba1",
			Changes: []template.TemplateWebhookChange{
				{
					Field: template.FieldMessageTemplateStatusUpdate,
					Value: template.TemplateWebhookValue{
						Event:                   "APPROVED",
						MessageTemplateID:       1400,
						MessageTemplateName:     "promo_a",
						MessageTemplateLanguage: "en_US",
						Reason:                  "NONE",
						MessageTemplateCategory: "MARKETING",
					},
				},
				{
					Field: template.FieldMessageTemplateStatusUpdate,
					Value: template.TemplateWebhookValue{
						Event:                   "REJECTED",
						MessageTemplateID:       1401,
						MessageTemplateName:     "promo_b",
						MessageTemplateLanguage: "en_US",
						Reason:                  "ABUSIVE_CONTENT",
						MessageTemplateCategory: "MARKETING",
					},
				},
			},
		}},
	}

	if err := uc.Execute(payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if repo.templates["t1"].Status != template.TemplateStatusApproved {
		t.Errorf("t1 status = %s, want APPROVED", repo.templates["t1"].Status)
	}
	if repo.templates["t2"].Status != template.TemplateStatusRejected {
		t.Errorf("t2 status = %s, want REJECTED", repo.templates["t2"].Status)
	}
}

func TestEventToStatus(t *testing.T) {
	tests := []struct {
		event  string
		want   template.TemplateStatus
		wantOk bool
	}{
		{"APPROVED", template.TemplateStatusApproved, true},
		{"REINSTATED", template.TemplateStatusApproved, true},
		{"REJECTED", template.TemplateStatusRejected, true},
		{"PAUSED", template.TemplateStatusPaused, true},
		{"LOCKED", template.TemplateStatusPaused, true},
		{"DISABLED", template.TemplateStatusDisabled, true},
		{"PENDING", template.TemplateStatusPending, true},
		{"IN_APPEAL", template.TemplateStatusPending, true},
		{"FLAGGED", "", false},
		{"ARCHIVED", "", false},
		{"LIMIT_EXCEEDED", "", false},
		{"UNKNOWN_EVENT", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		got, ok := eventToStatus(tt.event)
		if ok != tt.wantOk || got != tt.want {
			t.Errorf("eventToStatus(%q) = (%s, %v), want (%s, %v)", tt.event, got, ok, tt.want, tt.wantOk)
		}
	}
}
