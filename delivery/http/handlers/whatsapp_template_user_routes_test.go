package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	whatsapptemplatehttp "vozko/delivery/http/whatsapptemplate"
	"vozko/domain/auth"
	"vozko/domain/media"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/template"
	"vozko/domain/workspace_template_access"
	"vozko/infra/http/middleware"
)

type stubTemplateListUC struct {
	result *shared.PaginatedResult[*template.Template]
	err    error
}

func (s *stubTemplateListUC) Execute(input template.ListInput) (*shared.PaginatedResult[*template.Template], error) {
	return s.result, s.err
}

type stubTemplateGetUC struct {
	tmpl *template.Template
	err  error
}

func (s *stubTemplateGetUC) Execute(templateID string) (*template.Template, error) {
	return s.tmpl, s.err
}

type stubTemplateSyncUC struct {
	templates []*template.Template
	err       error
}

func (s *stubTemplateSyncUC) Execute(input template.SyncTemplatesInput) ([]*template.Template, error) {
	return s.templates, s.err
}

type stubTemplateSyncOneUC struct {
	tmpl *template.Template
	err  error
}

func (s *stubTemplateSyncOneUC) Execute(input template.SyncTemplateInput) (*template.Template, error) {
	return s.tmpl, s.err
}

type stubTemplateSendUC struct {
	result *template.SendTemplateResult
	err    error
}

func (s *stubTemplateSendUC) Execute(input template.SendTemplateMessageInput) (*template.SendTemplateResult, error) {
	return s.result, s.err
}

type stubTemplateCreateUC struct {
	capturedInput template.CreateTemplateInput
	result        *template.CreateTemplateOutput
	err           error
}

func (s *stubTemplateCreateUC) Execute(input template.CreateTemplateInput) (*template.CreateTemplateOutput, error) {
	s.capturedInput = input
	return s.result, s.err
}

type stubTemplateReplicateUC struct {
	result *template.CreateTemplateOutput
	err    error
}

func (s *stubTemplateReplicateUC) Execute(input template.ReplicateTemplateInput) (*template.CreateTemplateOutput, error) {
	return s.result, s.err
}

type stubTemplateUpdateHeaderMediaUC struct {
	err error
}

func (s *stubTemplateUpdateHeaderMediaUC) Execute(input template.SetTemplateHeaderMediaInput) error {
	return s.err
}

type stubTemplateDeleteUC struct {
	capturedID string
	err        error
}

func (s *stubTemplateDeleteUC) Execute(input template.DeleteTemplateInput) error {
	s.capturedID = input.TemplateID
	return s.err
}

type stubPhoneRepoForTemplates struct {
	phones map[string]*businessphone.WhatsAppBusinessPhoneNumber
	err    error
}

func (s *stubPhoneRepoForTemplates) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if s.err != nil {
		return nil, s.err
	}
	phone, ok := s.phones[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return phone, nil
}

func (s *stubPhoneRepoForTemplates) Create(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForTemplates) Update(_ string, _ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForTemplates) Delete(_ string) error { return nil }
func (s *stubPhoneRepoForTemplates) FindByMetaPhoneNumberID(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForTemplates) FindByMetaPhoneNumberIDUnscoped(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForTemplates) FindByDisplayPhoneNumber(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForTemplates) FindByWABAId(wabaID string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	var result []*businessphone.WhatsAppBusinessPhoneNumber
	for _, p := range s.phones {
		if p.WABAId == wabaID {
			result = append(result, p)
		}
	}
	return result, nil
}
func (s *stubPhoneRepoForTemplates) List(_ businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return nil, nil
}
func (s *stubPhoneRepoForTemplates) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForTemplates) BatchUpdate(_ []*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForTemplates) UpdateStatus(_ string, _ businessphone.Status) error { return nil }
func (s *stubPhoneRepoForTemplates) UpdateCallsEnabled(_ string, _ bool) error           { return nil }
func (s *stubPhoneRepoForTemplates) UpdateBusinessProfile(_ string, _ businessphone.BusinessProfile) error {
	return nil
}
func (s *stubPhoneRepoForTemplates) SyncFromMeta(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForTemplates) ClearAccessToken(_ string) error { return nil }
func (s *stubPhoneRepoForTemplates) ClearOwner(_ string) error       { return nil }
func (s *stubPhoneRepoForTemplates) Restore(_ string) error          { return nil }

type stubTemplateAccessRepo struct {
	access      map[string]map[string]bool
	templateIDs map[string][]string
	createErr   error
}

func newStubTemplateAccessRepo() *stubTemplateAccessRepo {
	return &stubTemplateAccessRepo{
		access:      make(map[string]map[string]bool),
		templateIDs: make(map[string][]string),
	}
}

func (s *stubTemplateAccessRepo) Create(access *workspace_template_access.WorkspaceTemplateAccess) error {
	if s.createErr != nil {
		return s.createErr
	}
	if s.access[access.WorkspaceID] == nil {
		s.access[access.WorkspaceID] = make(map[string]bool)
	}
	s.access[access.WorkspaceID][access.TemplateID] = true
	return nil
}

func (s *stubTemplateAccessRepo) Delete(_ string) error { return nil }
func (s *stubTemplateAccessRepo) DeleteByWorkspaceAndTemplate(_, _ string) error {
	return nil
}
func (s *stubTemplateAccessRepo) FindByID(_ string) (*workspace_template_access.WorkspaceTemplateAccess, error) {
	return nil, nil
}
func (s *stubTemplateAccessRepo) FindByWorkspaceAndTemplate(wsID, tplID string) (*workspace_template_access.WorkspaceTemplateAccess, error) {
	return nil, nil
}
func (s *stubTemplateAccessRepo) ListByWorkspace(_ workspace_template_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_template_access.WorkspaceTemplateAccess], error) {
	return nil, nil
}
func (s *stubTemplateAccessRepo) ListByTemplate(_ workspace_template_access.ListTemplateAccessInput) (*shared.PaginatedResult[*workspace_template_access.WorkspaceTemplateAccess], error) {
	return nil, nil
}
func (s *stubTemplateAccessRepo) GetTemplateIDsForWorkspace(wsID string) ([]string, error) {
	return s.templateIDs[wsID], nil
}
func (s *stubTemplateAccessRepo) HasAccess(wsID, tplID string) (bool, error) {
	if m, ok := s.access[wsID]; ok {
		return m[tplID], nil
	}
	return false, nil
}

func requestWithClaims(method, path string, body interface{}, claims *auth.Claims, wsID string) *http.Request {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, claims)
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, wsID)
	return req.WithContext(ctx)
}

func requestWithClaimsAndVars(method, path string, body interface{}, claims *auth.Claims, wsID string, vars map[string]string) *http.Request {
	req := requestWithClaims(method, path, body, claims, wsID)
	return mux.SetURLVars(req, vars)
}

func userClaims(userID string) *auth.Claims {
	return &auth.Claims{
		UserID: userID,
		Email:  userID + "@test.com",
		Role:   "user",
	}
}

func adminClaims() *auth.Claims {
	return &auth.Claims{
		UserID: "admin-user",
		Email:  "admin@test.com",
		Role:   "admin",
	}
}

func newTemplateTestHandler(opts ...func(*templateTestOpts)) *whatsapptemplatehttp.WhatsAppTemplateHandler {
	o := &templateTestOpts{
		createUC:           &stubTemplateCreateUC{result: &template.CreateTemplateOutput{ID: "tpl-1", ExternalID: "ext-1", Name: "test", Status: "PENDING"}},
		deleteUC:           &stubTemplateDeleteUC{},
		syncUC:             &stubTemplateSyncUC{templates: []*template.Template{}},
		syncOneUC:          &stubTemplateSyncOneUC{tmpl: &template.Template{ID: "tpl-1"}},
		updateMediaUC:      &stubTemplateUpdateHeaderMediaUC{},
		getUC:              &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", WABAId: "waba-1"}},
		listUC:             &stubTemplateListUC{result: &shared.PaginatedResult[*template.Template]{}},
		templateAccessRepo: newStubTemplateAccessRepo(),
		fileStorage:        &stubFileStorage{},
		phoneRepo: &stubPhoneRepoForTemplates{
			phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{
				"phone-ws1":      {ID: "phone-ws1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1", AccessToken: "token-1"},
				"phone-ws2":      {ID: "phone-ws2", OwnerWorkspaceID: "ws-2", WABAId: "waba-2", AccessToken: "token-2"},
				"phone-vozko":    {ID: "phone-vozko", OwnerWorkspaceID: "ws-1", WABAId: "waba-sys", AccessToken: "token-vozko"},
				"phone-no-owner": {ID: "phone-no-owner", OwnerWorkspaceID: "", WABAId: "waba-noowner", AccessToken: "tok"},
			},
		},
	}
	for _, fn := range opts {
		fn(o)
	}
	return whatsapptemplatehttp.NewWhatsAppTemplateHandler(
		o.listUC,
		o.getUC,
		o.syncUC,
		o.syncOneUC,
		&stubTemplateSendUC{},
		o.createUC,
		&stubTemplateReplicateUC{},
		o.updateMediaUC,
		o.deleteUC,
		nil,
		o.templateAccessRepo,
		o.phoneRepo,
	)
}

type templateTestOpts struct {
	createUC           *stubTemplateCreateUC
	deleteUC           *stubTemplateDeleteUC
	syncUC             *stubTemplateSyncUC
	syncOneUC          *stubTemplateSyncOneUC
	updateMediaUC      *stubTemplateUpdateHeaderMediaUC
	getUC              *stubTemplateGetUC
	listUC             *stubTemplateListUC
	templateAccessRepo *stubTemplateAccessRepo
	phoneRepo          *stubPhoneRepoForTemplates
	fileStorage        media.FileStorage
}

func TestCreateForWorkspace_Success(t *testing.T) {
	createUC := &stubTemplateCreateUC{
		result: &template.CreateTemplateOutput{ID: "tpl-1", ExternalID: "ext-1", Name: "hello_world", Status: "PENDING"},
	}
	accessRepo := newStubTemplateAccessRepo()
	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.createUC = createUC
		o.templateAccessRepo = accessRepo
	})

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-ws1",
		Name:            "hello_world",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	if !accessRepo.access["ws-1"]["tpl-1"] {
		t.Error("expected workspace template access to be auto-granted")
	}
}

func TestCreateForWorkspace_PhoneNotOwnedByWorkspace(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-ws2",
		Name:            "test",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateForWorkspace_PhoneNotFound(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "nonexistent",
		Name:            "test",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateForWorkspace_PhoneWithNoOwner(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-no-owner",
		Name:            "test",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateForWorkspace_NoClaims(t *testing.T) {
	h := newTemplateTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/whatsapp/templates", nil)
	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestCreateForWorkspace_MissingPhoneID(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		Name:     "test",
		Category: "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateForWorkspace_MissingName(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-ws1",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateForWorkspace_MissingCategory(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-ws1",
		Name:            "test",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateForWorkspace_ValidationError(t *testing.T) {
	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.createUC = &stubTemplateCreateUC{err: template.ErrTemplateNameInvalidChars}
	})

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-ws1",
		Name:            "INVALID-NAME!",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateForWorkspace_PreviouslySystemWABAAllowed(t *testing.T) {

	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-vozko",
		Name:            "test",
		Category:        "MARKETING",
	}, userClaims("user-1"), "ws-1")

	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 for workspace-owned phone, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteForWorkspace_Success(t *testing.T) {
	deleteUC := &stubTemplateDeleteUC{}
	getUC := &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", Name: "test_tpl", WABAId: "waba-1"}}
	accessRepo := newStubTemplateAccessRepo()
	accessRepo.access["ws-1"] = map[string]bool{"tpl-1": true}

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.deleteUC = deleteUC
		o.getUC = getUC
		o.templateAccessRepo = accessRepo
	})

	req := requestWithClaimsAndVars(http.MethodDelete, "/whatsapp/templates/tpl-1", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.DeleteForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteForWorkspace_NoAccess(t *testing.T) {
	getUC := &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", WABAId: "waba-2"}}
	accessRepo := newStubTemplateAccessRepo()

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.getUC = getUC
		o.templateAccessRepo = accessRepo
	})

	req := requestWithClaimsAndVars(http.MethodDelete, "/whatsapp/templates/tpl-1", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.DeleteForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteForWorkspace_PhoneNotOwnedByWorkspace(t *testing.T) {

	getUC := &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", Name: "test_tpl", WABAId: "waba-2"}}
	accessRepo := newStubTemplateAccessRepo()
	accessRepo.access["ws-1"] = map[string]bool{"tpl-1": true}

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.getUC = getUC
		o.templateAccessRepo = accessRepo
		o.phoneRepo = &stubPhoneRepoForTemplates{
			phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{
				"phone-ws2": {ID: "phone-ws2", OwnerWorkspaceID: "ws-2", WABAId: "waba-2"},
			},
		}
	})

	req := requestWithClaimsAndVars(http.MethodDelete, "/whatsapp/templates/tpl-1", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.DeleteForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestDeleteForWorkspace_NoClaims(t *testing.T) {
	h := newTemplateTestHandler()
	req := httptest.NewRequest(http.MethodDelete, "/whatsapp/templates/tpl-1", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.DeleteForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestDeleteForWorkspace_MissingID(t *testing.T) {
	h := newTemplateTestHandler()
	req := requestWithClaimsAndVars(http.MethodDelete, "/whatsapp/templates/", nil, userClaims("user-1"), "ws-1", map[string]string{})
	rr := httptest.NewRecorder()
	h.DeleteForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteForWorkspace_TemplateNotFound(t *testing.T) {
	accessRepo := newStubTemplateAccessRepo()
	accessRepo.access["ws-1"] = map[string]bool{"tpl-1": true}

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.getUC = &stubTemplateGetUC{err: template.ErrTemplateNotFound}
		o.templateAccessRepo = accessRepo
	})

	req := requestWithClaimsAndVars(http.MethodDelete, "/whatsapp/templates/tpl-1", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.DeleteForWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSyncForWorkspace_Success(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates/sync?businessPhoneId=phone-ws1", nil, userClaims("user-1"), "ws-1")
	rr := httptest.NewRecorder()
	h.SyncForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSyncForWorkspace_PhoneNotOwned(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates/sync?businessPhoneId=phone-ws2", nil, userClaims("user-1"), "ws-1")
	rr := httptest.NewRecorder()
	h.SyncForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSyncForWorkspace_MissingPhoneID(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates/sync", nil, userClaims("user-1"), "ws-1")
	rr := httptest.NewRecorder()
	h.SyncForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSyncForWorkspace_NoClaims(t *testing.T) {
	h := newTemplateTestHandler()

	req := httptest.NewRequest(http.MethodPost, "/whatsapp/templates/sync?businessPhoneId=phone-ws1", nil)
	rr := httptest.NewRecorder()
	h.SyncForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSyncOneForWorkspace_Success(t *testing.T) {
	accessRepo := newStubTemplateAccessRepo()
	accessRepo.access["ws-1"] = map[string]bool{"tpl-1": true}

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.templateAccessRepo = accessRepo
		o.getUC = &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", WABAId: "waba-1"}}
	})

	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/templates/tpl-1/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSyncOneForWorkspace_NoAccess(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/templates/tpl-1/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateHeaderMediaForWorkspace_Success(t *testing.T) {
	accessRepo := newStubTemplateAccessRepo()
	accessRepo.access["ws-1"] = map[string]bool{"tpl-1": true}

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.templateAccessRepo = accessRepo
		o.getUC = &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", WABAId: "waba-1"}}
	})

	url := "https://example.com/image.jpg"
	req := requestWithClaimsAndVars(http.MethodPatch, "/whatsapp/templates/tpl-1/header-media", map[string]*string{"headerMediaUrl": &url}, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.UpdateHeaderMediaForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateHeaderMediaForWorkspace_NoAccess(t *testing.T) {
	h := newTemplateTestHandler()

	req := requestWithClaimsAndVars(http.MethodPatch, "/whatsapp/templates/tpl-1/header-media", map[string]string{}, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.UpdateHeaderMediaForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateHeaderMediaForWorkspace_PhoneNotOwnedByWorkspace(t *testing.T) {
	accessRepo := newStubTemplateAccessRepo()
	accessRepo.access["ws-1"] = map[string]bool{"tpl-1": true}

	h := newTemplateTestHandler(func(o *templateTestOpts) {
		o.templateAccessRepo = accessRepo
		o.getUC = &stubTemplateGetUC{tmpl: &template.Template{ID: "tpl-1", WABAId: "waba-2"}}
		o.phoneRepo = &stubPhoneRepoForTemplates{
			phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{
				"phone-ws2": {ID: "phone-ws2", OwnerWorkspaceID: "ws-2", WABAId: "waba-2"},
			},
		}
	})

	req := requestWithClaimsAndVars(http.MethodPatch, "/whatsapp/templates/tpl-1/header-media", map[string]string{}, userClaims("user-1"), "ws-1", map[string]string{"id": "tpl-1"})
	rr := httptest.NewRecorder()
	h.UpdateHeaderMediaForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

type stubFileStorage struct {
	uploads map[string][]byte
}

func (s *stubFileStorage) UploadFile(key string, data []byte) error {
	if s.uploads == nil {
		s.uploads = map[string][]byte{}
	}
	s.uploads[key] = data
	return nil
}

func (s *stubFileStorage) GetFileURL(key string) string {
	return "https://cdn.example.test/" + key
}

func multipartFile(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

// The header of a PNG is enough: the handler resolves the type from the .png
// extension, so no full image is needed to exercise the happy path.
var onePixelPNGHeader = []byte("\x89PNG\r\n\x1a\n")

func TestUploadHeaderMedia_Success(t *testing.T) {
	storage := &stubFileStorage{}
	h := newTemplateTestHandler(func(o *templateTestOpts) { o.fileStorage = storage })

	body, contentType := multipartFile(t, "file", "banner.png", onePixelPNGHeader)
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/templates/header-media/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	h.UploadHeaderMedia(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		URL         string `json:"url"`
		ContentType string `json:"contentType"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rr.Body.String())
	}
	if resp.URL == "" {
		t.Fatalf("expected a url, got body=%s", rr.Body.String())
	}
	if resp.ContentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", resp.ContentType)
	}
	if len(storage.uploads) != 1 {
		t.Fatalf("expected exactly one stored object, got %d", len(storage.uploads))
	}
	for k := range storage.uploads {
		if !strings.HasPrefix(k, "template-headers/") {
			t.Errorf("stored key %q should be under template-headers/", k)
		}
	}
}

func TestUploadHeaderMedia_UnsupportedType(t *testing.T) {
	storage := &stubFileStorage{}
	h := newTemplateTestHandler(func(o *templateTestOpts) { o.fileStorage = storage })

	body, contentType := multipartFile(t, "file", "notes.txt", []byte("just text, not a media asset"))
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/templates/header-media/upload", body)
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	h.UploadHeaderMedia(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415; body=%s", rr.Code, rr.Body.String())
	}
	if len(storage.uploads) != 0 {
		t.Errorf("unsupported upload must not be stored, got %d objects", len(storage.uploads))
	}
}

func TestUploadHeaderMedia_MissingFile(t *testing.T) {
	h := newTemplateTestHandler(func(o *templateTestOpts) { o.fileStorage = &stubFileStorage{} })

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("something", "else")
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/templates/header-media/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rr := httptest.NewRecorder()

	h.UploadHeaderMedia(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}
