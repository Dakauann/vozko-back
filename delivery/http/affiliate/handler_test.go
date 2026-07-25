package affiliate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	affiliatedomain "vozko/domain/affiliate"
	"vozko/domain/auth"
	"vozko/infra/http/middleware"
)

type mockRegisterUC struct {
	in  affiliatedomain.RegisterAffiliateInput
	out *affiliatedomain.Affiliate
	err error
}

func (m *mockRegisterUC) Execute(_ context.Context, in affiliatedomain.RegisterAffiliateInput) (*affiliatedomain.Affiliate, error) {
	m.in = in
	return m.out, m.err
}

type mockGetMyUC struct {
	uid string
	out *affiliatedomain.AffiliateProfileWithStats
	err error
}

func (m *mockGetMyUC) Execute(_ context.Context, uid string) (*affiliatedomain.AffiliateProfileWithStats, error) {
	m.uid = uid
	return m.out, m.err
}

type mockUpdateMyUC struct {
	in  affiliatedomain.UpdateAffiliateProfileInput
	out *affiliatedomain.Affiliate
	err error
}

func (m *mockUpdateMyUC) Execute(_ context.Context, in affiliatedomain.UpdateAffiliateProfileInput) (*affiliatedomain.Affiliate, error) {
	m.in = in
	return m.out, m.err
}

type mockListReferrUC struct {
	uid        string
	page, size int
	items      []affiliatedomain.Referral
	total      int64
	err        error
}

func (m *mockListReferrUC) Execute(_ context.Context, uid string, page, size int) ([]affiliatedomain.Referral, int64, error) {
	m.uid, m.page, m.size = uid, page, size
	return m.items, m.total, m.err
}

type mockListEarnUC struct {
	uid        string
	page, size int
	items      []affiliatedomain.Earning
	total      int64
	err        error
}

func (m *mockListEarnUC) Execute(_ context.Context, uid string, page, size int) ([]affiliatedomain.Earning, int64, error) {
	m.uid, m.page, m.size = uid, page, size
	return m.items, m.total, m.err
}

type mockStatsUC struct {
	out *affiliatedomain.Stats
	err error
}

func (m *mockStatsUC) Execute(_ context.Context, _ string) (*affiliatedomain.Stats, error) {
	return m.out, m.err
}

type mockValidateUC struct {
	code string
	out  *affiliatedomain.ReferralValidationResult
	err  error
}

func (m *mockValidateUC) Execute(_ context.Context, code string) (*affiliatedomain.ReferralValidationResult, error) {
	m.code = code
	return m.out, m.err
}

type mockAdminListUC struct {
	page, size int
	items      []affiliatedomain.Affiliate
	total      int64
	err        error
}

func (m *mockAdminListUC) Execute(_ context.Context, page, size int) ([]affiliatedomain.Affiliate, int64, error) {
	m.page, m.size = page, size
	return m.items, m.total, m.err
}

type mockAdminGetUC struct {
	id  string
	out *affiliatedomain.AffiliateProfileWithStats
	err error
}

func (m *mockAdminGetUC) Execute(_ context.Context, id string) (*affiliatedomain.AffiliateProfileWithStats, error) {
	m.id = id
	return m.out, m.err
}

type mockAdminUpdateUC struct {
	in  affiliatedomain.AdminUpdateAffiliateInput
	out *affiliatedomain.Affiliate
	err error
}

func (m *mockAdminUpdateUC) Execute(_ context.Context, in affiliatedomain.AdminUpdateAffiliateInput) (*affiliatedomain.Affiliate, error) {
	m.in = in
	return m.out, m.err
}

type harness struct {
	h            *AffiliateHandler
	reg          *mockRegisterUC
	getMy        *mockGetMyUC
	updateMy     *mockUpdateMyUC
	listReferr   *mockListReferrUC
	listEarn     *mockListEarnUC
	stats        *mockStatsUC
	validateCode *mockValidateUC
	adminList    *mockAdminListUC
	adminGet     *mockAdminGetUC
	adminUpdate  *mockAdminUpdateUC
}

func newHarness() *harness {
	h := &harness{
		reg:          &mockRegisterUC{},
		getMy:        &mockGetMyUC{},
		updateMy:     &mockUpdateMyUC{},
		listReferr:   &mockListReferrUC{},
		listEarn:     &mockListEarnUC{},
		stats:        &mockStatsUC{},
		validateCode: &mockValidateUC{},
		adminList:    &mockAdminListUC{},
		adminGet:     &mockAdminGetUC{},
		adminUpdate:  &mockAdminUpdateUC{},
	}
	h.h = NewAffiliateHandler(
		h.reg, h.getMy, h.updateMy, h.listReferr, h.listEarn, h.stats,
		h.validateCode, h.adminList, h.adminGet, h.adminUpdate,
	)
	return h
}

func withClaims(r *http.Request, uid string) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: uid})
	return r.WithContext(ctx)
}

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func TestAffiliateRegister_Unauthorized(t *testing.T) {
	h := newHarness()
	req := httptest.NewRequest("POST", "/affiliate/register", nil)
	w := httptest.NewRecorder()
	h.h.Register(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestAffiliateRegister_InvalidBody(t *testing.T) {
	h := newHarness()
	req := withClaims(httptest.NewRequest("POST", "/affiliate/register", bytes.NewReader([]byte("not-json"))), "u1")
	w := httptest.NewRecorder()
	h.h.Register(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestAffiliateRegister_Success(t *testing.T) {
	h := newHarness()
	h.reg.out = &affiliatedomain.Affiliate{ID: "a1"}
	body := jsonBody(t, map[string]string{
		"asaasWalletId": "w",
		"brandName":     "Brand Name",
		"brandLogoUrl":  "https://cdn/x.png",
		"code":          "MY-CODE",
	})
	req := withClaims(httptest.NewRequest("POST", "/affiliate/register", body), "u1")
	w := httptest.NewRecorder()
	h.h.Register(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if h.reg.in.UserID != "u1" {
		t.Fatalf("uid=%q", h.reg.in.UserID)
	}
}

func TestAffiliateRegister_DomainErrors(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{affiliatedomain.ErrAffiliateNotFound, http.StatusNotFound},
		{affiliatedomain.ErrAffiliateAlreadyExists, http.StatusConflict},
		{affiliatedomain.ErrCodeAlreadyTaken, http.StatusConflict},
		{affiliatedomain.ErrWorkspaceAlreadyReferred, http.StatusConflict},
		{affiliatedomain.ErrInvalidCode, http.StatusBadRequest},
		{affiliatedomain.ErrInvalidBrandName, http.StatusBadRequest},
		{affiliatedomain.ErrInvalidBrandLogoURL, http.StatusBadRequest},
		{affiliatedomain.ErrInvalidAsaasWalletID, http.StatusBadRequest},
		{affiliatedomain.ErrInvalidCommissionPct, http.StatusBadRequest},
		{affiliatedomain.ErrInvalidReferralCode, http.StatusBadRequest},
		{affiliatedomain.ErrSelfReferral, http.StatusBadRequest},
		{affiliatedomain.ErrUnauthorized, http.StatusUnauthorized},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		h := newHarness()
		h.reg.err = c.err
		body := jsonBody(t, map[string]string{})
		req := withClaims(httptest.NewRequest("POST", "/affiliate/register", body), "u1")
		w := httptest.NewRecorder()
		h.h.Register(w, req)
		if w.Code != c.code {
			t.Fatalf("err=%v want=%d got=%d", c.err, c.code, w.Code)
		}
	}
}

func TestAffiliateGetMe_Unauthorized(t *testing.T) {
	h := newHarness()
	w := httptest.NewRecorder()
	h.h.GetMe(w, httptest.NewRequest("GET", "/affiliate/me", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateGetMe_Success(t *testing.T) {
	h := newHarness()
	h.getMy.out = &affiliatedomain.AffiliateProfileWithStats{Affiliate: &affiliatedomain.Affiliate{ID: "a"}, Stats: &affiliatedomain.Stats{}}
	req := withClaims(httptest.NewRequest("GET", "/affiliate/me", nil), "u1")
	w := httptest.NewRecorder()
	h.h.GetMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateGetMe_Error(t *testing.T) {
	h := newHarness()
	h.getMy.err = affiliatedomain.ErrAffiliateNotFound
	req := withClaims(httptest.NewRequest("GET", "/affiliate/me", nil), "u1")
	w := httptest.NewRecorder()
	h.h.GetMe(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateUpdateMe_Unauthorized(t *testing.T) {
	h := newHarness()
	w := httptest.NewRecorder()
	h.h.UpdateMe(w, httptest.NewRequest("PUT", "/affiliate/me", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateUpdateMe_InvalidBody(t *testing.T) {
	h := newHarness()
	req := withClaims(httptest.NewRequest("PUT", "/affiliate/me", bytes.NewReader([]byte("x"))), "u1")
	w := httptest.NewRecorder()
	h.h.UpdateMe(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateUpdateMe_Success(t *testing.T) {
	h := newHarness()
	h.updateMy.out = &affiliatedomain.Affiliate{ID: "a"}
	req := withClaims(httptest.NewRequest("PUT", "/affiliate/me", jsonBody(t, map[string]any{"brandName": "B"})), "u1")
	w := httptest.NewRecorder()
	h.h.UpdateMe(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateUpdateMe_Error(t *testing.T) {
	h := newHarness()
	h.updateMy.err = affiliatedomain.ErrInvalidBrandName
	req := withClaims(httptest.NewRequest("PUT", "/affiliate/me", jsonBody(t, map[string]any{})), "u1")
	w := httptest.NewRecorder()
	h.h.UpdateMe(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateListReferrals_Unauthorized(t *testing.T) {
	h := newHarness()
	w := httptest.NewRecorder()
	h.h.ListReferrals(w, httptest.NewRequest("GET", "/affiliate/referrals", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateListReferrals_Success(t *testing.T) {
	h := newHarness()
	h.listReferr.items = []affiliatedomain.Referral{{ID: "r1"}}
	h.listReferr.total = 1
	req := withClaims(httptest.NewRequest("GET", "/affiliate/referrals?page=1&pageSize=10", nil), "u1")
	w := httptest.NewRecorder()
	h.h.ListReferrals(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateListReferrals_Error(t *testing.T) {
	h := newHarness()
	h.listReferr.err = errors.New("db")
	req := withClaims(httptest.NewRequest("GET", "/affiliate/referrals", nil), "u1")
	w := httptest.NewRecorder()
	h.h.ListReferrals(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateListEarnings_Unauthorized(t *testing.T) {
	h := newHarness()
	w := httptest.NewRecorder()
	h.h.ListEarnings(w, httptest.NewRequest("GET", "/affiliate/earnings", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateListEarnings_Success(t *testing.T) {
	h := newHarness()
	h.listEarn.items = []affiliatedomain.Earning{{ID: "e1"}}
	h.listEarn.total = 1
	req := withClaims(httptest.NewRequest("GET", "/affiliate/earnings", nil), "u1")
	w := httptest.NewRecorder()
	h.h.ListEarnings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateListEarnings_Error(t *testing.T) {
	h := newHarness()
	h.listEarn.err = errors.New("db")
	req := withClaims(httptest.NewRequest("GET", "/affiliate/earnings", nil), "u1")
	w := httptest.NewRecorder()
	h.h.ListEarnings(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("%d", w.Code)
	}
}

func TestAffiliateValidateCode_Success(t *testing.T) {
	h := newHarness()
	h.validateCode.out = &affiliatedomain.ReferralValidationResult{Valid: true, Code: "ABC"}
	req := mux.SetURLVars(httptest.NewRequest("GET", "/affiliate/validate/ABC", nil), map[string]string{"code": "ABC"})
	w := httptest.NewRecorder()
	h.h.ValidateCode(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	if h.validateCode.code != "ABC" {
		t.Fatalf("code=%q", h.validateCode.code)
	}
}

func TestAffiliateValidateCode_Error(t *testing.T) {
	h := newHarness()
	h.validateCode.err = errors.New("boom")
	req := mux.SetURLVars(httptest.NewRequest("GET", "/affiliate/validate/X", nil), map[string]string{"code": "X"})
	w := httptest.NewRecorder()
	h.h.ValidateCode(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("%d", w.Code)
	}
}

func TestAdminList_Success(t *testing.T) {
	h := newHarness()
	h.adminList.items = []affiliatedomain.Affiliate{{ID: "a"}}
	h.adminList.total = 1
	req := httptest.NewRequest("GET", "/admin/affiliates", nil)
	w := httptest.NewRecorder()
	h.h.AdminList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
}

func TestAdminList_Error(t *testing.T) {
	h := newHarness()
	h.adminList.err = errors.New("db")
	w := httptest.NewRecorder()
	h.h.AdminList(w, httptest.NewRequest("GET", "/admin/affiliates", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("%d", w.Code)
	}
}

func TestAdminGet_Success(t *testing.T) {
	h := newHarness()
	h.adminGet.out = &affiliatedomain.AffiliateProfileWithStats{Affiliate: &affiliatedomain.Affiliate{}, Stats: &affiliatedomain.Stats{}}
	req := mux.SetURLVars(httptest.NewRequest("GET", "/admin/affiliates/a1", nil), map[string]string{"id": "a1"})
	w := httptest.NewRecorder()
	h.h.AdminGet(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d", w.Code)
	}
	if h.adminGet.id != "a1" {
		t.Fatalf("id=%q", h.adminGet.id)
	}
}

func TestAdminGet_Error(t *testing.T) {
	h := newHarness()
	h.adminGet.err = affiliatedomain.ErrAffiliateNotFound
	req := mux.SetURLVars(httptest.NewRequest("GET", "/admin/affiliates/x", nil), map[string]string{"id": "x"})
	w := httptest.NewRecorder()
	h.h.AdminGet(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("%d", w.Code)
	}
}

func TestAdminUpdate_InvalidBody(t *testing.T) {
	h := newHarness()
	req := mux.SetURLVars(httptest.NewRequest("PUT", "/admin/affiliates/a", bytes.NewReader([]byte("x"))), map[string]string{"id": "a"})
	w := httptest.NewRecorder()
	h.h.AdminUpdate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}

func TestAdminUpdate_Success(t *testing.T) {
	h := newHarness()
	h.adminUpdate.out = &affiliatedomain.Affiliate{ID: "a"}
	pct := 0.05
	req := mux.SetURLVars(httptest.NewRequest("PUT", "/admin/affiliates/a", jsonBody(t, map[string]any{"commissionPct": pct})), map[string]string{"id": "a"})
	w := httptest.NewRecorder()
	h.h.AdminUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d body=%s", w.Code, w.Body.String())
	}
	if h.adminUpdate.in.ID != "a" {
		t.Fatalf("id=%q", h.adminUpdate.in.ID)
	}
}

func TestAdminUpdate_Error(t *testing.T) {
	h := newHarness()
	h.adminUpdate.err = affiliatedomain.ErrInvalidCommissionPct
	req := mux.SetURLVars(httptest.NewRequest("PUT", "/admin/affiliates/a", jsonBody(t, map[string]any{})), map[string]string{"id": "a"})
	w := httptest.NewRecorder()
	h.h.AdminUpdate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%d", w.Code)
	}
}

func TestAdminUpdate_TierForwarded(t *testing.T) {
	h := newHarness()
	h.adminUpdate.out = &affiliatedomain.Affiliate{ID: "a", Tier: affiliatedomain.TierReseller}
	req := mux.SetURLVars(
		httptest.NewRequest("PUT", "/admin/affiliates/a", jsonBody(t, map[string]any{"tier": "reseller"})),
		map[string]string{"id": "a"},
	)
	w := httptest.NewRecorder()
	h.h.AdminUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if h.adminUpdate.in.Tier == nil || *h.adminUpdate.in.Tier != affiliatedomain.TierReseller {
		t.Fatalf("tier not forwarded: %+v", h.adminUpdate.in.Tier)
	}
}

func TestAdminUpdate_TierOmittedNotForwarded(t *testing.T) {
	h := newHarness()
	h.adminUpdate.out = &affiliatedomain.Affiliate{ID: "a"}
	req := mux.SetURLVars(
		httptest.NewRequest("PUT", "/admin/affiliates/a", jsonBody(t, map[string]any{"active": true})),
		map[string]string{"id": "a"},
	)
	w := httptest.NewRecorder()
	h.h.AdminUpdate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	if h.adminUpdate.in.Tier != nil {
		t.Fatalf("tier must be nil when omitted, got %v", *h.adminUpdate.in.Tier)
	}
}

func TestAdminUpdate_InvalidTier400(t *testing.T) {
	h := newHarness()
	h.adminUpdate.err = affiliatedomain.ErrInvalidTier
	req := mux.SetURLVars(
		httptest.NewRequest("PUT", "/admin/affiliates/a", jsonBody(t, map[string]any{"tier": "wholesaler"})),
		map[string]string{"id": "a"},
	)
	w := httptest.NewRecorder()
	h.h.AdminUpdate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for ErrInvalidTier, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAffiliatePagesFor(t *testing.T) {
	if pagesFor(0, 10) != 0 {
		t.Fatal("zero total")
	}
	if pagesFor(10, 0) != 0 {
		t.Fatal("zero size")
	}
	if pagesFor(10, 10) != 1 {
		t.Fatal("exact")
	}
	if pagesFor(11, 10) != 2 {
		t.Fatal("over")
	}
}
