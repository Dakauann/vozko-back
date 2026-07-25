package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	authdomain "vozko/domain/auth"
)

type stubCredentialsLogin struct {
	pair *authdomain.TokenPair
	err  error
}

func (s *stubCredentialsLogin) Execute(input authdomain.CredentialsInput) (*authdomain.TokenPair, error) {
	return s.pair, s.err
}

type stubRegister struct {
	pair *authdomain.TokenPair
	err  error
}

func (s *stubRegister) Execute(input authdomain.CredentialsInput) (*authdomain.TokenPair, error) {
	return s.pair, s.err
}

type stubAdminRegister struct {
	pair *authdomain.TokenPair
	err  error
}

func (s *stubAdminRegister) Execute(input authdomain.CredentialsInput) (*authdomain.TokenPair, error) {
	return s.pair, s.err
}

type stubRefreshToken struct {
	pair *authdomain.TokenPair
	err  error
}

func (s *stubRefreshToken) Execute(refreshToken string, ipAddress string, deviceInfo string) (*authdomain.TokenPair, error) {
	return s.pair, s.err
}

type stubRequestPasswordReset struct {
	err error
}

func (s *stubRequestPasswordReset) Execute(input authdomain.RequestPasswordResetInput) error {
	return s.err
}

type stubResetPassword struct {
	err error
}

func (s *stubResetPassword) Execute(input authdomain.ResetPasswordInput) error {
	return s.err
}

type stubSendEmailVerification struct {
	err error
}

func (s *stubSendEmailVerification) Execute(email string) error {
	return s.err
}

type stubVerifyEmail struct {
	err error
}

func (s *stubVerifyEmail) Execute(token string) error {
	return s.err
}

func defaultTokenPair() *authdomain.TokenPair {
	return &authdomain.TokenPair{
		AccessToken:  "at-123",
		RefreshToken: "rt-123",
		UserID:       "user-1",
		Email:        "user@test.com",
		Name:         "Test User",
		Role:         "user",
		CustomerType: "individual",
	}
}

func newTestHandler() *AuthHandler {
	return NewAuthHandler(
		&stubCredentialsLogin{pair: defaultTokenPair()},

		&stubRegister{pair: defaultTokenPair()},
		&stubAdminRegister{pair: &authdomain.TokenPair{UserID: "u1", Email: "u@t.com", Name: "U", Role: "user", CustomerType: "individual"}},

		&stubRefreshToken{pair: defaultTokenPair()},
		&stubRequestPasswordReset{},
		&stubResetPassword{},
		&stubSendEmailVerification{},
		&stubVerifyEmail{},
		nil, nil, nil, nil, nil)

}

func postJSON(handler http.HandlerFunc, path string, body interface{}) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func getRequest(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestLogin_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "password123",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["accessToken"] != "at-123" {
		t.Errorf("expected accessToken at-123, got %v", resp["accessToken"])
	}
	if resp["tokenType"] != "Bearer" {
		t.Errorf("expected tokenType Bearer, got %v", resp["tokenType"])
	}
}

func TestLogin_MissingEmail(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"password": "password123",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestLogin_MissingPassword(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestLogin_MissingBoth(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Login, "/auth/login", map[string]string{})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestLogin_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()
	h.Login(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	h := NewAuthHandler(
		&stubCredentialsLogin{err: authdomain.ErrInvalidCredentials}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "wrong",
	})

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestLogin_InternalError(t *testing.T) {
	h := NewAuthHandler(
		&stubCredentialsLogin{err: errors.New("db error")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "user@test.com",
		"password": "pass",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestRefreshToken_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.RefreshToken, "/auth/refresh", map[string]string{
		"refreshToken": "valid-rt",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRefreshToken_Missing(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.RefreshToken, "/auth/refresh", map[string]string{})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRefreshToken_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()
	h.RefreshToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRefreshToken_Invalid(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil,
		&stubRefreshToken{err: authdomain.ErrInvalidCredentials}, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.RefreshToken, "/auth/refresh", map[string]string{
		"refreshToken": "bad-rt",
	})

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRefreshToken_InternalError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil,
		&stubRefreshToken{err: errors.New("db error")}, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.RefreshToken, "/auth/refresh", map[string]string{
		"refreshToken": "rt",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestForgotPassword_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.ForgotPassword, "/auth/forgot-password", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["message"] == nil {
		t.Error("expected message in response")
	}
}

func TestForgotPassword_MissingEmail(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.ForgotPassword, "/auth/forgot-password", map[string]string{})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestForgotPassword_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/forgot-password", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()
	h.ForgotPassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"token":       "123456",
		"newPassword": "NewStrong1",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestResetPassword_MissingToken(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"newPassword": "NewStrong1",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_MissingNewPassword(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"token": "123456",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/reset-password", bytes.NewReader([]byte("bad")))
	rr := httptest.NewRecorder()
	h.ResetPassword(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_InvalidToken(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil,
		&stubResetPassword{err: authdomain.ErrInvalidResetToken},
		nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"token":       "bad",
		"newPassword": "NewStrong1",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_UsedToken(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil,
		&stubResetPassword{err: authdomain.ErrResetTokenUsed},
		nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"token":       "used",
		"newPassword": "NewStrong1",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_WeakPassword(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil,
		&stubResetPassword{err: authdomain.ErrWeakPassword},
		nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"token":       "123456",
		"newPassword": "weak",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestResetPassword_InternalError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil,
		&stubResetPassword{err: errors.New("db error")},
		nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.ResetPassword, "/auth/reset-password", map[string]string{
		"token":       "123456",
		"newPassword": "Strong1Pass",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestSendEmailVerification_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}
}

func TestSendEmailVerification_MissingEmail(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestSendEmailVerification_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/send-email-verification", bytes.NewReader([]byte("bad")))
	rr := httptest.NewRecorder()
	h.SendEmailVerification(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestSendEmailVerification_RateLimit(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil,
		&stubSendEmailVerification{err: authdomain.ErrEmailVerificationRateExceeded}, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
}

func TestSendEmailVerification_InternalError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil,
		&stubSendEmailVerification{err: errors.New("redis error")}, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestVerifyEmail_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.VerifyEmail, "/auth/verify-email", map[string]string{
		"token": "123456",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestVerifyEmail_MissingToken(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.VerifyEmail, "/auth/verify-email", map[string]string{})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestVerifyEmail_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/verify-email", bytes.NewReader([]byte("bad")))
	rr := httptest.NewRecorder()
	h.VerifyEmail(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestVerifyEmail_InvalidToken(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil, nil,
		&stubVerifyEmail{err: authdomain.ErrInvalidVerificationToken}, nil, nil, nil, nil, nil)

	rr := postJSON(h.VerifyEmail, "/auth/verify-email", map[string]string{
		"token": "bad",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestVerifyEmail_UsedToken(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil, nil,
		&stubVerifyEmail{err: authdomain.ErrVerificationTokenUsed}, nil, nil, nil, nil, nil)

	rr := postJSON(h.VerifyEmail, "/auth/verify-email", map[string]string{
		"token": "used",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestVerifyEmail_InternalError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil, nil,
		&stubVerifyEmail{err: errors.New("db error")}, nil, nil, nil, nil, nil)

	rr := postJSON(h.VerifyEmail, "/auth/verify-email", map[string]string{
		"token": "123456",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestRegister_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test User",
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRegister_MissingName(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_MissingEmail(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test User",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader([]byte("bad")))
	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_NameTooShort(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "A",
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_MissingCustomerType(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test User",
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_InvalidCustomerType(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test User",
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"customerType":      "unknown",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_IndividualMissingCPF(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test User",
		"email":             "user@test.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_CompanyMissingCNPJ(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Company",
		"email":             "c@test.com",
		"password":          "StrongPass1",
		"customerType":      "company",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_EmailAlreadyExists(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrEmailAlreadyExists}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "taken@test.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestRegister_InvalidVerificationToken(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrInvalidVerificationToken}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "bad-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_DocumentAlreadyExists(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrDocumentAlreadyExists}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "valid-token",
	})

	// A duplicate document is a client conflict, not a server error: the handler
	// maps authdomain.ErrDocumentAlreadyExists to 409 (this assertion previously still
	// expected the old 500 and was stale on both main and this branch).
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestRegister_MissingVerificationToken(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":         "Test User",
		"email":        "user@test.com",
		"password":     "StrongPass1",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_Success(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin Created",
		"email":        "admin@test.com",
		"password":     "AdminPass1",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
}

func TestAdminRegister_MissingFields(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"email": "admin@test.com",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/admin/users/register", bytes.NewReader([]byte("bad")))
	rr := httptest.NewRecorder()
	h.AdminRegister(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_NameTooShort(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "A",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_EmailAlreadyExists(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: authdomain.ErrEmailAlreadyExists}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "existing@test.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestRegister_VerificationTokenUsed(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrVerificationTokenUsed}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "used-token",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_MissingVerificationTokenError(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrMissingVerificationToken}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_InvalidCustomerTypeError(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrInvalidCustomerType}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_MissingCustomerDocumentError(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrMissingCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_InvalidCustomerDocumentError(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrInvalidCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rr.Code)
	}
}

func TestRegister_InvalidCustomerDocumentError_Company(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrInvalidCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Company",
		"email":             "c@t.com",
		"password":          "StrongPass1",
		"customerType":      "company",
		"cnpj":              "12345678901234",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", rr.Code)
	}
}

func TestRegister_MissingCustomerDocumentError_Company(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrMissingCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Company",
		"email":             "c@t.com",
		"password":          "StrongPass1",
		"customerType":      "company",
		"cnpj":              "12345678901234",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_InternalError(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: errors.New("db error")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestRegister_NameTooLong(t *testing.T) {
	h := newTestHandler()
	longName := ""
	for i := 0; i < 130; i++ {
		longName += "a"
	}
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              longName,
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestRegister_CompanySuccess(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Company Test",
		"email":             "company@test.com",
		"password":          "StrongPass1",
		"customerType":      "company",
		"cnpj":              "12345678901234",
		"verificationToken": "valid-token",
	})

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRegister_MissingPassword(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "x",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_InvalidCustomerType(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "unknown",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_MissingCustomerType(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":     "Admin",
		"email":    "a@t.com",
		"password": "Pass123",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_IndividualMissingCPF(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_CompanyMissingCNPJ(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Company",
		"email":        "c@t.com",
		"password":     "Pass123",
		"customerType": "company",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_CompanySuccess(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Company",
		"email":        "c@t.com",
		"password":     "Pass123",
		"customerType": "company",
		"cnpj":         "12345678901234",
	})

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rr.Code)
	}
}

func TestAdminRegister_NameTooLong(t *testing.T) {
	h := newTestHandler()
	longName := ""
	for i := 0; i < 130; i++ {
		longName += "a"
	}
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         longName,
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_InvalidCustomerTypeError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: authdomain.ErrInvalidCustomerType}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_MissingDocumentError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: authdomain.ErrMissingCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_InvalidDocumentError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: authdomain.ErrInvalidCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_InvalidDocumentError_Company(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: authdomain.ErrInvalidCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Company",
		"email":        "c@t.com",
		"password":     "Pass123",
		"customerType": "company",
		"cnpj":         "12345678901234",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_MissingDocumentError_Company(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: authdomain.ErrMissingCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Company",
		"email":        "c@t.com",
		"password":     "Pass123",
		"customerType": "company",
		"cnpj":         "12345678901234",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_InternalError(t *testing.T) {
	h := NewAuthHandler(
		nil, nil,
		&stubAdminRegister{err: errors.New("db error")}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

func TestAdminRegister_MissingEmail(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"password":     "Pass123",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestAdminRegister_MissingPassword(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.AdminRegister, "/admin/users/register", map[string]string{
		"name":         "Admin",
		"email":        "a@t.com",
		"customerType": "individual",
		"cpf":          "12345678901",
	})

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func decodeErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unable to decode error body %q: %v", string(body), err)
	}
	return parsed.Code
}

func TestLogin_InvalidCredentialsEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		&stubCredentialsLogin{err: authdomain.ErrInvalidCredentials},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "u@t.com",
		"password": "wrong",
	})

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeInvalidCredentials {
		t.Errorf("expected code %q, got %q", AuthCodeInvalidCredentials, got)
	}
}

func TestLogin_InternalErrorEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		&stubCredentialsLogin{err: errors.New("db down")},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Login, "/auth/login", map[string]string{
		"email":    "u@t.com",
		"password": "any",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeInternal {
		t.Errorf("expected code %q, got %q", AuthCodeInternal, got)
	}
}

func TestRegister_EmailExistsEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrEmailAlreadyExists}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "123456",
	})

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeEmailAlreadyExists {
		t.Errorf("expected code %q, got %q", AuthCodeEmailAlreadyExists, got)
	}
}

func TestRegister_InvalidVerificationTokenEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrInvalidVerificationToken}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "999999",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeInvalidVerificationTok {
		t.Errorf("expected code %q, got %q", AuthCodeInvalidVerificationTok, got)
	}
}

func TestRegister_VerificationTokenUsedEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrVerificationTokenUsed}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "12345678901",
		"verificationToken": "123456",
	})

	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeVerificationTokUsed {
		t.Errorf("expected code %q, got %q", AuthCodeVerificationTokUsed, got)
	}
}

func TestRegister_InvalidDocumentEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		nil,
		&stubRegister{err: authdomain.ErrInvalidCustomerDocument}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.Register, "/auth/register", map[string]string{
		"name":              "Test",
		"email":             "u@t.com",
		"password":          "StrongPass1",
		"customerType":      "individual",
		"cpf":               "00000000000",
		"verificationToken": "123456",
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeInvalidDocument {
		t.Errorf("expected code %q, got %q", AuthCodeInvalidDocument, got)
	}
}

func TestSendEmailVerification_RateLimitEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil,
		&stubSendEmailVerification{err: authdomain.ErrEmailVerificationRateExceeded}, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeRateLimitExceeded {
		t.Errorf("expected code %q, got %q", AuthCodeRateLimitExceeded, got)
	}
}

func TestSendEmailVerification_DeliveryFailureEmitsCode(t *testing.T) {
	h := NewAuthHandler(
		nil, nil, nil, nil, nil, nil,
		&stubSendEmailVerification{err: errors.New("smtp down")}, nil, nil, nil, nil, nil, nil)

	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if got := decodeErrorCode(t, rr.Body.Bytes()); got != AuthCodeEmailDeliveryFail {
		t.Errorf("expected code %q, got %q", AuthCodeEmailDeliveryFail, got)
	}
}

func TestSendEmailVerification_SuccessReturns202(t *testing.T) {
	h := newTestHandler()
	rr := postJSON(h.SendEmailVerification, "/auth/send-email-verification", map[string]string{
		"email": "user@test.com",
	})

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}
}
