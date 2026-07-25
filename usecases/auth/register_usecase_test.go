package auth_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/auth"
	"vozko/domain/business_metrics"
	"vozko/domain/customer"
	"vozko/domain/shared"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type mockUserRepo struct {
	users      map[string]*user.User
	byDoc      map[string]*user.User
	createErr  error
	createdCPF string
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users: make(map[string]*user.User),
		byDoc: make(map[string]*user.User),
	}
}

func (r *mockUserRepo) WithTx(tx interface{}) user.UserRepository { return r }
func (r *mockUserRepo) Create(u *user.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	u.ID = "new-user-id"
	r.users[u.Email] = u
	r.createdCPF = u.CPF
	return nil
}
func (r *mockUserRepo) Update(string, *user.User) error { return nil }
func (r *mockUserRepo) Delete(string) error             { return nil }
func (r *mockUserRepo) FindByID(string) (*user.User, error) {
	return nil, errors.New("not found")
}
func (r *mockUserRepo) FindByIDs([]string) ([]*user.User, error) { return nil, nil }
func (r *mockUserRepo) FindByEmail(email string) (*user.User, error) {
	if u, ok := r.users[email]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}
func (r *mockUserRepo) FindByDocument(doc string) (*user.User, error) {
	if u, ok := r.byDoc[doc]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}
func (r *mockUserRepo) List(user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	return nil, nil
}
func (r *mockUserRepo) CountByRole(user.Role) (int64, error)      { return 0, nil }
func (r *mockUserRepo) GetUserRole(string) (string, error)        { return "", nil }
func (r *mockUserRepo) GetTokenVersion(string) (int, error)       { return 0, nil }
func (r *mockUserRepo) IncrementTokenVersion(string) (int, error) { return 1, nil }

type mockPasswordService struct{}

func (s *mockPasswordService) Hash(plain string) (string, error) { return "hashed:" + plain, nil }
func (s *mockPasswordService) Verify(string, string) error       { return nil }

type mockTokenIssuer struct{}

func (t *mockTokenIssuer) Issue(u *user.User) (*auth.TokenPair, error) {
	return &auth.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: "",
		UserID:       u.ID,
		Email:        u.Email,
		AccessJTI:    "jti-test",
	}, nil
}
func (t *mockTokenIssuer) GenerateRefreshToken() (string, string, error) {
	return "raw-refresh", "hash-refresh", nil
}
func (t *mockTokenIssuer) HashRefreshToken(raw string) string {
	return "hashed-" + raw
}

type mockEmailService struct{}

func (s *mockEmailService) SendEmail(string, string, string) error { return nil }
func (s *mockEmailService) SendTemplate(string, string, string, map[string]interface{}) error {
	return nil
}

type mockDocValidator struct{}

func (v *mockDocValidator) ValidateCPFOrCNPJ(doc string) bool {
	return len(doc) == 11 || len(doc) == 14
}
func (v *mockDocValidator) Normalize(doc string) string { return doc }

type mockVerifyToken struct{}

func (v *mockVerifyToken) Execute(string) error { return nil }

type mockTokenRepo struct{}

func (r *mockTokenRepo) Create(*auth.EmailVerificationToken) error { return nil }
func (r *mockTokenRepo) FindByToken(string) (*auth.EmailVerificationToken, error) {
	return nil, nil
}
func (r *mockTokenRepo) MarkAsUsed(string) error                                 { return nil }
func (r *mockTokenRepo) DeleteExpired() error                                    { return nil }
func (r *mockTokenRepo) CountByEmailInWindow(string, time.Duration) (int, error) { return 0, nil }

type mockCustomerRepo struct{}

func (r *mockCustomerRepo) CreateCustomer(*customer.Customer) error            { return nil }
func (r *mockCustomerRepo) GetCustomerByID(string) (*customer.Customer, error) { return nil, nil }
func (r *mockCustomerRepo) GetCustomerByDocument(string) (*customer.Customer, error) {
	return nil, nil
}
func (r *mockCustomerRepo) GetCustomerByDocumentEmailOrPhone(string, string, string) (*customer.Customer, error) {
	return nil, nil
}
func (r *mockCustomerRepo) GetCustomerByEmail(string) (*customer.Customer, error) { return nil, nil }
func (r *mockCustomerRepo) GetCustomerByPhone(string) (*customer.Customer, error) { return nil, nil }
func (r *mockCustomerRepo) UpdateCustomer(*customer.Customer) error               { return nil }
func (r *mockCustomerRepo) ListCustomersByUser(string) ([]customer.Customer, error) {
	return nil, nil
}

type mockRecordMetric struct{}

type mockSessionRepo struct{}

func (r *mockSessionRepo) Create(*auth.Session) error                                 { return nil }
func (r *mockSessionRepo) FindByID(string) (*auth.Session, error)                     { return nil, nil }
func (r *mockSessionRepo) FindByRefreshTokenHash(string) (*auth.Session, error)          { return nil, nil }
func (r *mockSessionRepo) FindByPreviousRefreshTokenHash(string) (*auth.Session, error)  { return nil, nil }
func (r *mockSessionRepo) FindByAccessJTI(string, string) (*auth.Session, error)         { return nil, nil }
func (r *mockSessionRepo) FindActiveByUserID(string) ([]*auth.Session, error)            { return nil, nil }
func (r *mockSessionRepo) UpdateRefreshToken(string, string, string, string, time.Time) (int64, error) {
	return 1, nil
}
func (r *mockSessionRepo) UpdateSessionInfo(string, string, string) error             { return nil }
func (r *mockSessionRepo) Revoke(string) error                                        { return nil }
func (r *mockSessionRepo) RevokeAllByUserID(string) error                             { return nil }
func (r *mockSessionRepo) DeleteExpired() error                                       { return nil }

func (m *mockRecordMetric) Execute(business_metrics.RecordMetricInput) error { return nil }

func newTestRegisterUseCase(userRepo *mockUserRepo) auth.RegisterUseCase {
	return NewRegisterUseCase(
		userRepo,
		&mockPasswordService{},
		&mockTokenIssuer{},
		&mockSessionRepo{},
		&mockEmailService{},
		&mockDocValidator{},
		&mockVerifyToken{},
		&mockTokenRepo{},
		&mockCustomerRepo{},
		&mockRecordMetric{},
		nil,
	)
}

func TestRegister_IndividualWithUniqueCPF_Succeeds(t *testing.T) {
	repo := newMockUserRepo()
	uc := newTestRegisterUseCase(repo)

	tokens, err := uc.Execute(auth.CredentialsInput{
		Name:              "Alice",
		Email:             "alice@example.com",
		Password:          "StrongPassword1!",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokens == nil {
		t.Fatal("expected non-nil tokens")
	}
}

func TestRegister_IndividualWithDuplicateCPF_Fails(t *testing.T) {
	repo := newMockUserRepo()

	repo.byDoc["12345678901"] = &user.User{
		ID:           "existing-user",
		Email:        "bob@example.com",
		CPF:          "12345678901",
		CustomerType: user.CustomerTypeIndividual,
	}

	uc := newTestRegisterUseCase(repo)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "Alice",
		Email:             "alice@example.com",
		Password:          "StrongPassword1!",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})

	if !errors.Is(err, auth.ErrDocumentAlreadyExists) {
		t.Fatalf("expected ErrDocumentAlreadyExists, got: %v", err)
	}
}

func TestRegister_CompanyWithUniqueCNPJ_Succeeds(t *testing.T) {
	repo := newMockUserRepo()
	uc := newTestRegisterUseCase(repo)

	tokens, err := uc.Execute(auth.CredentialsInput{
		Name:              "Acme Inc",
		Email:             "acme@example.com",
		Password:          "StrongPassword1!",
		CustomerType:      "company",
		CNPJ:              "12345678000190",
		VerificationToken: "valid-token",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tokens == nil {
		t.Fatal("expected non-nil tokens")
	}
}

func TestRegister_CompanyWithDuplicateCNPJ_Fails(t *testing.T) {
	repo := newMockUserRepo()
	repo.byDoc["12345678000190"] = &user.User{
		ID:           "existing-company",
		Email:        "other@example.com",
		CNPJ:         "12345678000190",
		CustomerType: user.CustomerTypeCompany,
	}

	uc := newTestRegisterUseCase(repo)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "Acme Inc",
		Email:             "acme@example.com",
		Password:          "StrongPassword1!",
		CustomerType:      "company",
		CNPJ:              "12345678000190",
		VerificationToken: "valid-token",
	})

	if !errors.Is(err, auth.ErrDocumentAlreadyExists) {
		t.Fatalf("expected ErrDocumentAlreadyExists, got: %v", err)
	}
}

func TestRegister_SameCPFDifferentEmail_Fails(t *testing.T) {
	repo := newMockUserRepo()
	repo.byDoc["99988877766"] = &user.User{
		ID:    "user-1",
		Email: "first@example.com",
		CPF:   "99988877766",
	}

	uc := newTestRegisterUseCase(repo)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "Second Account",
		Email:             "second@example.com",
		Password:          "StrongPassword1!",
		CustomerType:      "individual",
		CPF:               "99988877766",
		VerificationToken: "valid-token",
	})

	if !errors.Is(err, auth.ErrDocumentAlreadyExists) {
		t.Fatalf("duplicate CPF across different emails should fail, got: %v", err)
	}
}

func TestRegister_DuplicateEmail_Fails(t *testing.T) {
	repo := newMockUserRepo()
	repo.users["taken@example.com"] = &user.User{
		ID:    "existing",
		Email: "taken@example.com",
	}

	uc := newTestRegisterUseCase(repo)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "Dup",
		Email:             "taken@example.com",
		Password:          "StrongPassword1!",
		CustomerType:      "individual",
		CPF:               "11122233344",
		VerificationToken: "valid-token",
	})

	if !errors.Is(err, auth.ErrEmailAlreadyExists) {
		t.Fatalf("expected ErrEmailAlreadyExists, got: %v", err)
	}
}

type stubEnsureDefaultWs struct {
	calls []struct {
		userID       string
		email        string
		referralCode string
	}
	err error
}

func (s *stubEnsureDefaultWs) Execute(userID, email, referralCode string) (*workspace.Workspace, error) {
	s.calls = append(s.calls, struct {
		userID       string
		email        string
		referralCode string
	}{userID, email, referralCode})
	return &workspace.Workspace{ID: "ws-" + userID, OwnerID: userID, Name: email, IsDefault: true}, s.err
}

func newRegisterUCWithEnsure(ensure workspace.EnsureDefaultWorkspaceUseCase) auth.RegisterUseCase {
	return NewRegisterUseCase(
		newMockUserRepo(),
		&mockPasswordService{},
		&mockTokenIssuer{},
		&mockSessionRepo{},
		&mockEmailService{},
		&mockDocValidator{},
		&mockVerifyToken{},
		&mockTokenRepo{},
		&mockCustomerRepo{},
		&mockRecordMetric{},
		ensure,
	)
}

func TestRegister_ReferralCode_ForwardedToEnsureDefault(t *testing.T) {
	ensure := &stubEnsureDefaultWs{}
	uc := newRegisterUCWithEnsure(ensure)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "new@example.com",
		Password:          "StrongPass1",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
		ReferralCode:      "  MYREF  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ensure.calls) != 1 {
		t.Fatalf("expected EnsureDefaultWorkspace to be called once, got %d", len(ensure.calls))
	}
	got := ensure.calls[0]
	if got.referralCode != "MYREF" {
		t.Fatalf("expected trimmed referral code MYREF, got %q", got.referralCode)
	}
	if got.email != "new@example.com" {
		t.Fatalf("expected email forwarded, got %q", got.email)
	}
}

func TestRegister_NoReferralCode_EmptyForwarded(t *testing.T) {
	ensure := &stubEnsureDefaultWs{}
	uc := newRegisterUCWithEnsure(ensure)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@example.com",
		Password:          "StrongPass1",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ensure.calls) != 1 {
		t.Fatalf("expected ensure called once")
	}
	if ensure.calls[0].referralCode != "" {
		t.Fatalf("expected empty code, got %q", ensure.calls[0].referralCode)
	}
}

func TestRegister_EnsureDefaultFailure_DoesNotBlock(t *testing.T) {
	ensure := &stubEnsureDefaultWs{err: errors.New("boom")}
	uc := newRegisterUCWithEnsure(ensure)

	tokens, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u2@example.com",
		Password:          "StrongPass1",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
		ReferralCode:      "X",
	})
	if err != nil {
		t.Fatalf("registration must succeed even if ensure fails, got: %v", err)
	}
	if tokens == nil {
		t.Fatal("expected non-nil token pair")
	}
}

func TestRegister_NilEnsureDefault_NoPanic(t *testing.T) {
	uc := newRegisterUCWithEnsure(nil)
	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "nil-ensure@example.com",
		Password:          "StrongPass1",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
		ReferralCode:      "X",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
