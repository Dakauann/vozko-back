package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	whatsappbusinessphonehttp "vozko/delivery/http/whatsappbusinessphone"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace_phone_access"
)

type stubPhoneSyncOneUC struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s *stubPhoneSyncOneUC) Execute(input businessphone.SyncPhoneNumberInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type stubPhoneRequestVerificationUC struct {
	err error
}

func (s *stubPhoneRequestVerificationUC) Execute(input businessphone.RequestVerificationCodeInput) error {
	return s.err
}

type stubPhoneVerifyCodeUC struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s *stubPhoneVerifyCodeUC) Execute(input businessphone.VerifyCodeInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type stubPhoneUpdateBusinessProfileUC struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s *stubPhoneUpdateBusinessProfileUC) Execute(input businessphone.UpdateBusinessProfileInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type stubPhoneGetBusinessProfileUC struct {
	profile *businessphone.BusinessProfile
	err     error
}

func (s *stubPhoneGetBusinessProfileUC) Execute(input businessphone.GetBusinessProfileInput) (*businessphone.BusinessProfile, error) {
	return s.profile, s.err
}

type stubPhoneListUC struct {
	result *shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]
	err    error
}

func (s *stubPhoneListUC) Execute(input businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return s.result, s.err
}

type stubPhoneGetUC struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s *stubPhoneGetUC) Execute(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type stubPhoneRegisterUC struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s *stubPhoneRegisterUC) Execute(input businessphone.RegisterPhoneInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type stubPhoneDeregisterUC struct {
	phone *businessphone.WhatsAppBusinessPhoneNumber
	err   error
}

func (s *stubPhoneDeregisterUC) Execute(input businessphone.DeregisterPhoneInput) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return s.phone, s.err
}

type stubPhoneReleaseUC struct {
	result *businessphone.ReleasePhoneResult
	err    error
}

func (s *stubPhoneReleaseUC) Execute(input businessphone.ReleasePhoneInput) (*businessphone.ReleasePhoneResult, error) {
	return s.result, s.err
}

type stubPhoneDeleteUC struct {
	err error
}

func (s *stubPhoneDeleteUC) Execute(id string) error {
	return s.err
}

type stubPhoneUnassignOwnerUC struct {
	err error
}

func (s *stubPhoneUnassignOwnerUC) Execute(id string) error {
	return s.err
}

type stubMetaAPIService struct{}

func (s *stubMetaAPIService) GetWABA(string, string) (*businessphone.MetaWABAInfo, error) {
	return nil, nil
}
func (s *stubMetaAPIService) ListPhoneNumbers(string, string) ([]businessphone.MetaPhoneNumberInfo, error) {
	return nil, nil
}
func (s *stubMetaAPIService) GetPhoneNumber(string, string) (*businessphone.MetaPhoneNumberInfo, error) {
	return nil, nil
}
func (s *stubMetaAPIService) RequestVerificationCode(string, string, string, string) error {
	return nil
}
func (s *stubMetaAPIService) VerifyCode(string, string, string) (bool, error) { return true, nil }
func (s *stubMetaAPIService) RegisterPhone(string, string, string) error      { return nil }
func (s *stubMetaAPIService) DeregisterPhone(string, string) error            { return nil }
func (s *stubMetaAPIService) UnsubscribeApp(string, string) error             { return nil }
func (s *stubMetaAPIService) GetBusinessProfile(string, string) (*businessphone.MetaBusinessProfile, error) {
	return nil, nil
}
func (s *stubMetaAPIService) UpdateBusinessProfile(string, businessphone.MetaBusinessProfile, string) error {
	return nil
}
func (s *stubMetaAPIService) UploadProfilePicture([]byte, string, string) (string, error) {
	return "handle-123", nil
}
func (s *stubMetaAPIService) GetCallingStatus(string, string) (bool, error) { return false, nil }
func (s *stubMetaAPIService) SetCallingStatus(string, bool, string) error   { return nil }
func (s *stubMetaAPIService) SubscribeWebhooks(string, string) error        { return nil }
func (s *stubMetaAPIService) BlockUser(string, string, string) error        { return nil }
func (s *stubMetaAPIService) UnblockUser(string, string, string) error      { return nil }

type stubPhoneRepoForPhone struct {
	phones map[string]*businessphone.WhatsAppBusinessPhoneNumber
}

func (s *stubPhoneRepoForPhone) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	phone, ok := s.phones[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return phone, nil
}
func (s *stubPhoneRepoForPhone) Create(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForPhone) Update(_ string, _ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForPhone) Delete(_ string) error { return nil }
func (s *stubPhoneRepoForPhone) FindByMetaPhoneNumberID(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForPhone) FindByMetaPhoneNumberIDUnscoped(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForPhone) FindByDisplayPhoneNumber(_ string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForPhone) FindByWABAId(_ string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForPhone) List(_ businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return nil, nil
}
func (s *stubPhoneRepoForPhone) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (s *stubPhoneRepoForPhone) BatchUpdate(_ []*businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForPhone) UpdateStatus(_ string, _ businessphone.Status) error { return nil }
func (s *stubPhoneRepoForPhone) UpdateCallsEnabled(_ string, _ bool) error           { return nil }
func (s *stubPhoneRepoForPhone) UpdateBusinessProfile(_ string, _ businessphone.BusinessProfile) error {
	return nil
}
func (s *stubPhoneRepoForPhone) SyncFromMeta(_ *businessphone.WhatsAppBusinessPhoneNumber) error {
	return nil
}
func (s *stubPhoneRepoForPhone) ClearAccessToken(_ string) error { return nil }
func (s *stubPhoneRepoForPhone) ClearOwner(_ string) error       { return nil }
func (s *stubPhoneRepoForPhone) Restore(_ string) error          { return nil }

type stubWorkspacePhoneAccessRepo struct{}

func (s *stubWorkspacePhoneAccessRepo) Create(_ *workspace_phone_access.WorkspacePhoneAccess) error {
	return nil
}
func (s *stubWorkspacePhoneAccessRepo) Delete(_ string) error                       { return nil }
func (s *stubWorkspacePhoneAccessRepo) DeleteByWorkspaceAndPhone(_, _ string) error { return nil }
func (s *stubWorkspacePhoneAccessRepo) FindByID(_ string) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	return nil, nil
}
func (s *stubWorkspacePhoneAccessRepo) FindByWorkspaceAndPhone(_, _ string) (*workspace_phone_access.WorkspacePhoneAccess, error) {
	return nil, nil
}
func (s *stubWorkspacePhoneAccessRepo) ListByWorkspace(_ workspace_phone_access.ListWorkspaceAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	return nil, nil
}
func (s *stubWorkspacePhoneAccessRepo) ListByPhone(_ workspace_phone_access.ListPhoneAccessInput) (*shared.PaginatedResult[*workspace_phone_access.WorkspacePhoneAccess], error) {
	return nil, nil
}
func (s *stubWorkspacePhoneAccessRepo) GetPhoneIDsForWorkspace(_ string) ([]string, error) {
	return nil, nil
}
func (s *stubWorkspacePhoneAccessRepo) HasAccess(_, _ string) (bool, error) { return false, nil }

func defaultPhoneRepo() *stubPhoneRepoForPhone {
	return &stubPhoneRepoForPhone{
		phones: map[string]*businessphone.WhatsAppBusinessPhoneNumber{
			"phone-ws1":      {ID: "phone-ws1", OwnerWorkspaceID: "ws-1", WABAId: "waba-1", AccessToken: "token-1", MetaPhoneNumberID: "meta-1"},
			"phone-ws2":      {ID: "phone-ws2", OwnerWorkspaceID: "ws-2", WABAId: "waba-2", AccessToken: "token-2", MetaPhoneNumberID: "meta-2"},
			"phone-vozko":    {ID: "phone-vozko", OwnerWorkspaceID: "ws-1", WABAId: "waba-sys", AccessToken: "token-vozko", MetaPhoneNumberID: "meta-vozko"},
			"phone-no-owner": {ID: "phone-no-owner", OwnerWorkspaceID: "", WABAId: "waba-noowner", AccessToken: "tok", MetaPhoneNumberID: "meta-no"},
		},
	}
}

type phoneTestOpts struct {
	phoneRepo             *stubPhoneRepoForPhone
	syncOneUC             *stubPhoneSyncOneUC
	requestVerificationUC *stubPhoneRequestVerificationUC
	verifyCodeUC          *stubPhoneVerifyCodeUC
	updateBusinessProfile *stubPhoneUpdateBusinessProfileUC
	getBusinessProfile    *stubPhoneGetBusinessProfileUC
}

func newPhoneTestHandler(opts ...func(*phoneTestOpts)) *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler {
	o := &phoneTestOpts{
		phoneRepo:             defaultPhoneRepo(),
		syncOneUC:             &stubPhoneSyncOneUC{phone: &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-ws1"}},
		requestVerificationUC: &stubPhoneRequestVerificationUC{},
		verifyCodeUC:          &stubPhoneVerifyCodeUC{phone: &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-ws1"}},
		updateBusinessProfile: &stubPhoneUpdateBusinessProfileUC{phone: &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-ws1"}},
		getBusinessProfile:    &stubPhoneGetBusinessProfileUC{profile: &businessphone.BusinessProfile{About: "test"}},
	}
	for _, fn := range opts {
		fn(o)
	}
	return whatsappbusinessphonehttp.NewWhatsAppBusinessPhoneHandler(
		whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandlerConfig{AccessToken: "sys-token"},
		&stubPhoneListUC{result: &shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber]{}},
		&stubPhoneGetUC{phone: &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-ws1"}},
		o.syncOneUC,
		&stubPhoneRegisterUC{},
		&stubPhoneDeregisterUC{},
		&stubPhoneReleaseUC{},
		o.requestVerificationUC,
		o.verifyCodeUC,
		o.updateBusinessProfile,
		o.getBusinessProfile,
		&stubPhoneDeleteUC{},
		&stubPhoneUnassignOwnerUC{},
		o.phoneRepo,
		&stubWorkspacePhoneAccessRepo{},
		&stubMetaAPIService{},
		nil, // whatsappClientFactory: unused by these Meta-provider tests
	)
}

func TestPhoneSyncOneForWorkspace_Success(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws1/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneSyncOneForWorkspace_PhoneNotOwned(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws2/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneSyncOneForWorkspace_PhoneNotFound(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/nonexistent/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "nonexistent"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneSyncOneForWorkspace_NoClaims(t *testing.T) {
	h := newPhoneTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/business-phones/phone-ws1/sync", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestPhoneSyncOneForWorkspace_VozkoPhoneBlocked(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-vozko/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-vozko"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneSyncOneForWorkspace_PhoneNoOwner(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-no-owner/sync", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-no-owner"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneRequestVerificationForWorkspace_Success(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{"method": "SMS", "language": "en_US"}
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws1/request-verification", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.RequestVerificationForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneRequestVerificationForWorkspace_PhoneNotOwned(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{"method": "SMS"}
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws2/request-verification", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.RequestVerificationForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneRequestVerificationForWorkspace_NoClaims(t *testing.T) {
	h := newPhoneTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/business-phones/phone-ws1/request-verification", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.RequestVerificationForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestPhoneVerifyCodeForWorkspace_Success(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{"code": "123456"}
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws1/verify-code", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.VerifyCodeForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneVerifyCodeForWorkspace_PhoneNotOwned(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{"code": "123456"}
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws2/verify-code", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.VerifyCodeForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneVerifyCodeForWorkspace_MissingCode(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{}
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws1/verify-code", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.VerifyCodeForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneVerifyCodeForWorkspace_NoClaims(t *testing.T) {
	h := newPhoneTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/whatsapp/business-phones/phone-ws1/verify-code", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.VerifyCodeForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestPhoneUpdateBusinessProfileForWorkspace_Success(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{"about": "My business", "address": "123 St"}
	req := requestWithClaimsAndVars(http.MethodPut, "/whatsapp/business-phones/phone-ws1/business-profile", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.UpdateBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneUpdateBusinessProfileForWorkspace_PhoneNotOwned(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]string{"about": "test"}
	req := requestWithClaimsAndVars(http.MethodPut, "/whatsapp/business-phones/phone-ws2/business-profile", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.UpdateBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneUpdateBusinessProfileForWorkspace_NoClaims(t *testing.T) {
	h := newPhoneTestHandler()
	req := httptest.NewRequest(http.MethodPut, "/whatsapp/business-phones/phone-ws1/business-profile", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.UpdateBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestPhoneGetBusinessProfileForWorkspace_Success(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodGet, "/whatsapp/business-phones/phone-ws1/business-profile", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.GetBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneGetBusinessProfileForWorkspace_PhoneNotOwned(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodGet, "/whatsapp/business-phones/phone-ws2/business-profile", nil, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.GetBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestPhoneGetBusinessProfileForWorkspace_NoClaims(t *testing.T) {
	h := newPhoneTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/whatsapp/business-phones/phone-ws1/business-profile", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "phone-ws1"})
	rr := httptest.NewRecorder()
	h.GetBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestPhoneGetBusinessProfileForWorkspace_MissingID(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodGet, "/whatsapp/business-phones//business-profile", nil, userClaims("user-1"), "ws-1", map[string]string{})
	rr := httptest.NewRecorder()
	h.GetBusinessProfileForWorkspace(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// --- Admin cross-workspace bypass on the shared verifyPhoneOwnership gate ---

// A global admin may toggle calling on a phone owned by another workspace.
func TestPhoneSetCallingStatus_AdminBypassesOwnership(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]bool{"enabled": true}
	// phone-ws2 is owned by ws-2; the admin's active workspace is ws-1.
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws2/calling-status", body, adminClaims(), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.SetCallingStatusForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin on non-owned phone, got %d: %s", rr.Code, rr.Body.String())
	}
}

// A non-admin still cannot act on a phone their workspace doesn't own.
func TestPhoneSetCallingStatus_NonAdminNonOwnedForbidden(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]bool{"enabled": true}
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws2/calling-status", body, userClaims("user-1"), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.SetCallingStatusForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin on non-owned phone, got %d: %s", rr.Code, rr.Body.String())
	}
}

// The Vozko-managed WABA block is enforced even for admins — the bypass only
// relaxes workspace ownership, not the managed-phone protection.
func TestPhoneSetCallingStatus_AdminVozkoManagedStillForbidden(t *testing.T) {
	h := newPhoneTestHandler()
	body := map[string]bool{"enabled": true}
	// phone-vozko rides the Vozko-managed WABA (waba-sys); admin acts from ws-2.
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-vozko/calling-status", body, adminClaims(), "ws-2", map[string]string{"id": "phone-vozko"})
	rr := httptest.NewRecorder()
	h.SetCallingStatusForWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for admin on Vozko-managed phone, got %d: %s", rr.Code, rr.Body.String())
	}
}

// The bypass is centralized in verifyPhoneOwnership, so it applies to every
// handler that gates through it — not just calling status.
func TestPhoneSyncOne_AdminBypassesOwnership(t *testing.T) {
	h := newPhoneTestHandler()
	req := requestWithClaimsAndVars(http.MethodPost, "/whatsapp/business-phones/phone-ws2/sync", nil, adminClaims(), "ws-1", map[string]string{"id": "phone-ws2"})
	rr := httptest.NewRecorder()
	h.SyncOneForWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin on non-owned phone, got %d: %s", rr.Code, rr.Body.String())
	}
}
