package auth_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/auth"
	"vozko/domain/business_metrics"
	"vozko/domain/cache"
	"vozko/domain/customer"
	"vozko/domain/shared"
	"vozko/domain/user"
)

type testUserRepo struct {
	users     map[string]*user.User
	byID      map[string]*user.User
	byDoc     map[string]*user.User
	createErr error
	updateErr error
}

func newTestUserRepo() *testUserRepo {
	return &testUserRepo{
		users: make(map[string]*user.User),
		byID:  make(map[string]*user.User),
		byDoc: make(map[string]*user.User),
	}
}

func (r *testUserRepo) WithTx(tx interface{}) user.UserRepository { return r }
func (r *testUserRepo) Create(u *user.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	if u.ID == "" {
		u.ID = "gen-" + u.Email
	}
	r.users[u.Email] = u
	r.byID[u.ID] = u
	return nil
}
func (r *testUserRepo) Update(id string, u *user.User) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.users[u.Email] = u
	r.byID[id] = u
	return nil
}
func (r *testUserRepo) Delete(string) error { return nil }
func (r *testUserRepo) FindByID(id string) (*user.User, error) {
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}
func (r *testUserRepo) FindByIDs(ids []string) ([]*user.User, error) { return nil, nil }
func (r *testUserRepo) FindByEmail(email string) (*user.User, error) {
	if u, ok := r.users[email]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}
func (r *testUserRepo) FindByDocument(doc string) (*user.User, error) {
	if u, ok := r.byDoc[doc]; ok {
		return u, nil
	}
	return nil, user.ErrNotFound
}
func (r *testUserRepo) List(user.ListUsersInput) (*shared.PaginatedResult[*user.User], error) {
	return nil, nil
}
func (r *testUserRepo) CountByRole(user.Role) (int64, error) { return 0, nil }
func (r *testUserRepo) GetUserRole(id string) (string, error) {
	if u, ok := r.byID[id]; ok {
		return string(u.Role), nil
	}
	return "", user.ErrNotFound
}
func (r *testUserRepo) GetTokenVersion(userId string) (int, error) {
	if u, ok := r.byID[userId]; ok {
		return u.TokenVersion, nil
	}
	return 0, user.ErrNotFound
}
func (r *testUserRepo) IncrementTokenVersion(userId string) (int, error) {
	if u, ok := r.byID[userId]; ok {
		u.TokenVersion++
		return u.TokenVersion, nil
	}
	return 0, user.ErrNotFound
}

type testPasswordService struct {
	hashErr   error
	verifyErr error
}

func (s *testPasswordService) Hash(plain string) (string, error) {
	if s.hashErr != nil {
		return "", s.hashErr
	}
	return "bcrypt:" + plain, nil
}
func (s *testPasswordService) Verify(hash, plain string) error {
	if s.verifyErr != nil {
		return s.verifyErr
	}
	if hash == "bcrypt:"+plain {
		return nil
	}
	return errors.New("mismatch")
}

type testTokenIssuer struct {
	issueErr           error
	generateRefreshErr error
}

func (t *testTokenIssuer) Issue(u *user.User) (*auth.TokenPair, error) {
	if t.issueErr != nil {
		return nil, t.issueErr
	}
	return &auth.TokenPair{
		AccessToken:  "at-" + u.ID,
		RefreshToken: "",
		UserID:       u.ID,
		Email:        u.Email,
		Name:         u.Username,
		Role:         string(u.Role),
		CustomerType: string(u.CustomerType),
		AccessJTI:    "jti-" + u.ID,
	}, nil
}
func (t *testTokenIssuer) GenerateRefreshToken() (string, string, error) {
	if t.generateRefreshErr != nil {
		return "", "", t.generateRefreshErr
	}
	return "raw-refresh-token", "hashed-refresh-token", nil
}
func (t *testTokenIssuer) HashRefreshToken(raw string) string {
	return "hashed-" + raw
}

type testEmailService struct {
	sentEmails []string
	sendErr    error
}

func (s *testEmailService) SendEmail(to, subject, body string) error {
	s.sentEmails = append(s.sentEmails, to)
	return s.sendErr
}
func (s *testEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	s.sentEmails = append(s.sentEmails, to)
	return s.sendErr
}

type testEmailPublisher struct {
	publishErr error
}

func (p *testEmailPublisher) Publish(userEmail, subject, template string, placeholders map[string]interface{}) error {
	return p.publishErr
}

type testDocValidator struct{}

func (v *testDocValidator) ValidateCPFOrCNPJ(doc string) bool {
	return len(doc) == 11 || len(doc) == 14
}
func (v *testDocValidator) Normalize(doc string) string { return doc }

type testVerifyEmailToken struct {
	err error
}

func (v *testVerifyEmailToken) Execute(token string) error { return v.err }

type testEmailVerifRepo struct {
	tokens    map[string]*auth.EmailVerificationToken
	countResp int
	countErr  error
	createErr error
}

func newTestEmailVerifRepo() *testEmailVerifRepo {
	return &testEmailVerifRepo{tokens: make(map[string]*auth.EmailVerificationToken)}
}

func (r *testEmailVerifRepo) Create(t *auth.EmailVerificationToken) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.tokens[t.Token] = t
	return nil
}
func (r *testEmailVerifRepo) FindByToken(token string) (*auth.EmailVerificationToken, error) {
	t, ok := r.tokens[token]
	if !ok {
		return nil, nil
	}
	return t, nil
}
func (r *testEmailVerifRepo) MarkAsUsed(token string) error {
	if t, ok := r.tokens[token]; ok {
		t.Used = true
	}
	return nil
}
func (r *testEmailVerifRepo) DeleteExpired() error { return nil }
func (r *testEmailVerifRepo) CountByEmailInWindow(email string, windowDuration time.Duration) (int, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.countResp, nil
}

type testPasswordResetRepo struct {
	tokens    map[string]*auth.PasswordResetToken // keyed by token ID
	markErr   error
	createErr error
	nextID    int
}

func newTestPasswordResetRepo() *testPasswordResetRepo {
	return &testPasswordResetRepo{tokens: make(map[string]*auth.PasswordResetToken)}
}

// seed inserts a token with a known ID for a user and returns it.
func (r *testPasswordResetRepo) seed(t *auth.PasswordResetToken) *auth.PasswordResetToken {
	if t.ID == "" {
		r.nextID++
		t.ID = "reset-" + string(rune('0'+r.nextID))
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	r.tokens[t.ID] = t
	return t
}

func (r *testPasswordResetRepo) Create(t *auth.PasswordResetToken) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.seed(t)
	return nil
}
func (r *testPasswordResetRepo) FindActiveByUserID(userID string) (*auth.PasswordResetToken, error) {
	var newest *auth.PasswordResetToken
	for _, t := range r.tokens {
		if t.UserID != userID || t.Used || time.Now().UTC().After(t.ExpiresAt) {
			continue
		}
		if newest == nil || t.CreatedAt.After(newest.CreatedAt) {
			newest = t
		}
	}
	if newest == nil {
		return nil, auth.ErrInvalidResetToken
	}
	return newest, nil
}
func (r *testPasswordResetRepo) IncrementAttempts(id string) (int, error) {
	if t, ok := r.tokens[id]; ok {
		t.Attempts++
		return t.Attempts, nil
	}
	return 0, auth.ErrInvalidResetToken
}
func (r *testPasswordResetRepo) MarkUsed(id string) error {
	if r.markErr != nil {
		return r.markErr
	}
	if t, ok := r.tokens[id]; ok {
		t.Used = true
	}
	return nil
}
func (r *testPasswordResetRepo) DeleteByUserID(userID string) error {
	for id, t := range r.tokens {
		if t.UserID == userID {
			delete(r.tokens, id)
		}
	}
	return nil
}
func (r *testPasswordResetRepo) DeleteExpired() error { return nil }

type testCustomerRepo struct{}

func (r *testCustomerRepo) CreateCustomer(*customer.Customer) error            { return nil }
func (r *testCustomerRepo) GetCustomerByID(string) (*customer.Customer, error) { return nil, nil }
func (r *testCustomerRepo) GetCustomerByDocument(string) (*customer.Customer, error) {
	return nil, nil
}
func (r *testCustomerRepo) GetCustomerByDocumentEmailOrPhone(string, string, string) (*customer.Customer, error) {
	return nil, nil
}
func (r *testCustomerRepo) GetCustomerByEmail(string) (*customer.Customer, error) { return nil, nil }
func (r *testCustomerRepo) GetCustomerByPhone(string) (*customer.Customer, error) { return nil, nil }
func (r *testCustomerRepo) UpdateCustomer(*customer.Customer) error               { return nil }
func (r *testCustomerRepo) ListCustomersByUser(string) ([]customer.Customer, error) {
	return nil, nil
}

type testRecordMetric struct {
	recorded []business_metrics.RecordMetricInput
}

func (m *testRecordMetric) Execute(input business_metrics.RecordMetricInput) error {
	m.recorded = append(m.recorded, input)
	return nil
}

type testSharedState struct{ data map[string]string }

func newTestSharedState() *testSharedState { return &testSharedState{data: make(map[string]string)} }
func (s *testSharedState) SetNX(k, v string, _ time.Duration) (bool, error) {
	s.data[k] = v
	return true, nil
}
func (s *testSharedState) SetString(k, v string, _ time.Duration) error { s.data[k] = v; return nil }
func (s *testSharedState) GetString(k string) (string, error) {
	v, ok := s.data[k]
	if !ok {
		return "", errors.New("miss")
	}
	return v, nil
}
func (s *testSharedState) Del(keys ...string) error {
	for _, k := range keys {
		delete(s.data, k)
	}
	return nil
}
func (s *testSharedState) Exists(k string) (bool, error)                    { _, ok := s.data[k]; return ok, nil }
func (s *testSharedState) Incr(string) (int64, error)                       { return 1, nil }
func (s *testSharedState) Decr(string) (int64, error)                       { return 0, nil }
func (s *testSharedState) IncrWithTTL(string, time.Duration) (int64, error) { return 1, nil }
func (s *testSharedState) TryIncr(string, int64) (bool, error)              { return true, nil }
func (s *testSharedState) SAdd(string, ...string) error                     { return nil }
func (s *testSharedState) SRem(string, ...string) error                     { return nil }
func (s *testSharedState) SMembers(string) ([]string, error)                { return nil, nil }
func (s *testSharedState) Publish(string, []byte) error                     { return nil }
func (s *testSharedState) Subscribe(context.Context, string, func([]byte))  {}
func (s *testSharedState) HSet(string, string, string) error                { return nil }
func (s *testSharedState) HDel(string, string) error                        { return nil }
func (s *testSharedState) HGetAll(string) (map[string]string, error)        { return nil, nil }
func (s *testSharedState) HIncrBy(string, string, int64) (int64, error)     { return 0, nil }
func (s *testSharedState) IncrBy(string, int64) (int64, error)              { return 0, nil }
func (s *testSharedState) DecrBy(string, int64) (int64, error)              { return 0, nil }
func (s *testSharedState) TryIncrBy(string, int64, int64) (bool, error)     { return true, nil }
func (s *testSharedState) Expire(string, time.Duration) (bool, error)       { return true, nil }

var _ cache.SharedState = (*testSharedState)(nil)

type testSessionRepo struct {
	sessions  map[string]*auth.Session
	byHash    map[string]*auth.Session
	createErr error
	revokeErr error
}

func newTestSessionRepo() *testSessionRepo {
	return &testSessionRepo{
		sessions: make(map[string]*auth.Session),
		byHash:   make(map[string]*auth.Session),
	}
}

func (r *testSessionRepo) Create(session *auth.Session) error {
	if r.createErr != nil {
		return r.createErr
	}
	if session.ID == "" {
		session.ID = "sess-" + session.UserID
	}
	r.sessions[session.ID] = session
	r.byHash[session.RefreshTokenHash] = session
	return nil
}
func (r *testSessionRepo) FindByID(id string) (*auth.Session, error) {
	if s, ok := r.sessions[id]; ok {
		return s, nil
	}
	return nil, auth.ErrSessionNotFound
}
func (r *testSessionRepo) FindByRefreshTokenHash(hash string) (*auth.Session, error) {
	if s, ok := r.byHash[hash]; ok {
		return s, nil
	}
	return nil, auth.ErrSessionNotFound
}
func (r *testSessionRepo) FindActiveByUserID(userID string) ([]*auth.Session, error) {
	var active []*auth.Session
	for _, s := range r.sessions {
		if s.UserID == userID && !s.IsRevoked() && !s.IsExpired() {
			active = append(active, s)
		}
	}
	return active, nil
}
func (r *testSessionRepo) FindByPreviousRefreshTokenHash(hash string) (*auth.Session, error) {
	for _, s := range r.sessions {
		if s.PreviousRefreshTokenHash == hash {
			return s, nil
		}
	}
	return nil, auth.ErrSessionNotFound
}
func (r *testSessionRepo) UpdateRefreshToken(sessionID string, expectedTokenHash string, newTokenHash string, newAccessJTI string, newExpiresAt time.Time) (int64, error) {
	s, ok := r.sessions[sessionID]
	if !ok {
		return 0, auth.ErrSessionNotFound
	}
	// Compare-and-swap: only rotate while the row still holds the expected hash.
	if s.RefreshTokenHash != expectedTokenHash {
		return 0, nil
	}
	delete(r.byHash, s.RefreshTokenHash)
	s.PreviousRefreshTokenHash = expectedTokenHash
	now := time.Now()
	s.RotatedAt = &now
	s.RefreshTokenHash = newTokenHash
	s.AccessJTI = newAccessJTI
	s.ExpiresAt = newExpiresAt
	r.byHash[newTokenHash] = s
	return 1, nil
}
func (r *testSessionRepo) UpdateSessionInfo(sessionID string, ipAddress string, deviceInfo string) error {
	if s, ok := r.sessions[sessionID]; ok {
		if ipAddress != "" {
			s.IPAddress = ipAddress
		}
		if deviceInfo != "" {
			s.DeviceInfo = deviceInfo
		}
		return nil
	}
	return auth.ErrSessionNotFound
}
func (r *testSessionRepo) Revoke(sessionID string) error {
	if r.revokeErr != nil {
		return r.revokeErr
	}
	if s, ok := r.sessions[sessionID]; ok {
		now := time.Now()
		s.RevokedAt = &now
		return nil
	}
	return auth.ErrSessionNotFound
}
func (r *testSessionRepo) RevokeAllByUserID(userID string) error {
	if r.revokeErr != nil {
		return r.revokeErr
	}
	now := time.Now()
	for _, s := range r.sessions {
		if s.UserID == userID {
			s.RevokedAt = &now
		}
	}
	return nil
}
func (r *testSessionRepo) DeleteExpired() error { return nil }
func (r *testSessionRepo) FindByAccessJTI(userID string, jti string) (*auth.Session, error) {
	for _, s := range r.sessions {
		if s.UserID == userID && s.AccessJTI == jti && !s.IsRevoked() {
			return s, nil
		}
	}
	return nil, auth.ErrSessionNotFound
}

func TestCredentialsLogin_Success(t *testing.T) {
	repo := newTestUserRepo()
	repo.users["user@test.com"] = &user.User{
		ID:       "u1",
		Email:    "user@test.com",
		Password: "bcrypt:password123",
		Role:     user.RoleUser,
	}
	repo.byID["u1"] = repo.users["user@test.com"]

	uc := NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
		&testRecordMetric{},
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Email:    "user@test.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair == nil {
		t.Fatal("expected non-nil token pair")
	}
	if pair.UserID != "u1" {
		t.Errorf("expected userID u1, got %s", pair.UserID)
	}
}

func TestCredentialsLogin_UserNotFound(t *testing.T) {
	repo := newTestUserRepo()
	uc := NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
		&testRecordMetric{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Email:    "nonexistent@test.com",
		Password: "password123",
	})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestCredentialsLogin_WrongPassword(t *testing.T) {
	repo := newTestUserRepo()
	repo.users["user@test.com"] = &user.User{
		ID:       "u1",
		Email:    "user@test.com",
		Password: "bcrypt:correctpass",
		Role:     user.RoleUser,
	}

	uc := NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
		&testRecordMetric{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Email:    "user@test.com",
		Password: "wrongpass",
	})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestCredentialsLogin_UserRepoError(t *testing.T) {
	repo := newTestUserRepo()

	repo.users["user@test.com"] = nil

	uc := NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
		&testRecordMetric{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Email:    "no-such@test.com",
		Password: "pass",
	})
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestCredentialsLogin_TokenIssueError(t *testing.T) {
	repo := newTestUserRepo()
	repo.users["user@test.com"] = &user.User{
		ID:       "u1",
		Email:    "user@test.com",
		Password: "bcrypt:pass",
		Role:     user.RoleUser,
	}

	uc := NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{issueErr: errors.New("token issue failed")},
		newTestSessionRepo(),
		&testEmailPublisher{},
		&testRecordMetric{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Email:    "user@test.com",
		Password: "pass",
	})
	if err == nil {
		t.Fatal("expected error for token issue failure")
	}
}

func TestCredentialsLogin_NilMetricRecorder(t *testing.T) {
	repo := newTestUserRepo()
	repo.users["user@test.com"] = &user.User{
		ID:       "u1",
		Email:    "user@test.com",
		Password: "bcrypt:pass",
		Role:     user.RoleUser,
	}

	uc := NewCredentialsLoginUseCase(
		repo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailPublisher{},
		nil,
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Email:    "user@test.com",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair == nil {
		t.Fatal("expected non-nil token pair")
	}
}

func TestRefreshToken_Success(t *testing.T) {
	repo := newTestUserRepo()
	repo.byID["u1"] = &user.User{ID: "u1", Email: "u@t.com", Role: user.RoleUser}

	sessionRepo := newTestSessionRepo()
	tokenIssuer := &testTokenIssuer{}

	rawToken := "raw-refresh-token"
	hash := tokenIssuer.HashRefreshToken(rawToken)
	sessionRepo.sessions["sess1"] = &auth.Session{
		ID:               "sess1",
		UserID:           "u1",
		RefreshTokenHash: hash,
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	sessionRepo.byHash[hash] = sessionRepo.sessions["sess1"]

	uc := NewRefreshTokenUseCase(repo, tokenIssuer, sessionRepo, newTestSharedState())

	pair, err := uc.Execute(rawToken, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.UserID != "u1" {
		t.Errorf("expected userID u1, got %s", pair.UserID)
	}
	if pair.RefreshToken == "" {
		t.Error("expected new refresh token after rotation")
	}
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	sessionRepo := newTestSessionRepo()

	uc := NewRefreshTokenUseCase(newTestUserRepo(), &testTokenIssuer{}, sessionRepo, newTestSharedState())

	_, err := uc.Execute("unknown-token", "", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestRefreshToken_RevokedSession(t *testing.T) {
	repo := newTestUserRepo()
	repo.byID["u1"] = &user.User{ID: "u1", Email: "u@t.com", Role: user.RoleUser}

	sessionRepo := newTestSessionRepo()
	tokenIssuer := &testTokenIssuer{}
	rawToken := "raw-revoked"
	hash := tokenIssuer.HashRefreshToken(rawToken)
	revokedAt := time.Now().Add(-1 * time.Hour)
	sessionRepo.sessions["sess1"] = &auth.Session{
		ID:               "sess1",
		UserID:           "u1",
		RefreshTokenHash: hash,
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		RevokedAt:        &revokedAt,
	}
	sessionRepo.byHash[hash] = sessionRepo.sessions["sess1"]

	uc := NewRefreshTokenUseCase(repo, tokenIssuer, sessionRepo, newTestSharedState())

	_, err := uc.Execute(rawToken, "", "")
	if !errors.Is(err, auth.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked, got: %v", err)
	}
}

func TestRefreshToken_ExpiredSession(t *testing.T) {
	repo := newTestUserRepo()
	repo.byID["u1"] = &user.User{ID: "u1", Email: "u@t.com", Role: user.RoleUser}

	sessionRepo := newTestSessionRepo()
	tokenIssuer := &testTokenIssuer{}
	rawToken := "raw-expired"
	hash := tokenIssuer.HashRefreshToken(rawToken)
	sessionRepo.sessions["sess1"] = &auth.Session{
		ID:               "sess1",
		UserID:           "u1",
		RefreshTokenHash: hash,
		ExpiresAt:        time.Now().Add(-1 * time.Hour),
	}
	sessionRepo.byHash[hash] = sessionRepo.sessions["sess1"]

	uc := NewRefreshTokenUseCase(repo, tokenIssuer, sessionRepo, newTestSharedState())

	_, err := uc.Execute(rawToken, "", "")
	if !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}
}

func TestRefreshToken_UserDeleted(t *testing.T) {
	repo := newTestUserRepo()

	sessionRepo := newTestSessionRepo()
	tokenIssuer := &testTokenIssuer{}
	rawToken := "raw-orphan"
	hash := tokenIssuer.HashRefreshToken(rawToken)
	sessionRepo.sessions["sess1"] = &auth.Session{
		ID:               "sess1",
		UserID:           "deleted-user",
		RefreshTokenHash: hash,
		ExpiresAt:        time.Now().Add(24 * time.Hour),
	}
	sessionRepo.byHash[hash] = sessionRepo.sessions["sess1"]

	uc := NewRefreshTokenUseCase(repo, tokenIssuer, sessionRepo, newTestSharedState())

	_, err := uc.Execute(rawToken, "", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got: %v", err)
	}
}

func TestRequestPasswordReset_Success(t *testing.T) {
	repo := newTestUserRepo()
	repo.users["user@test.com"] = &user.User{ID: "u1", Email: "user@test.com"}
	repo.byID["u1"] = repo.users["user@test.com"]

	resetRepo := newTestPasswordResetRepo()

	uc := NewRequestPasswordResetUseCase(
		repo,
		resetRepo,
		&testEmailService{},
	)

	err := uc.Execute(auth.RequestPasswordResetInput{Email: "user@test.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resetRepo.tokens) == 0 {
		t.Error("expected a reset token to be created")
	}
}

func TestRequestPasswordReset_UserNotFound_SilentSuccess(t *testing.T) {
	repo := newTestUserRepo()

	uc := NewRequestPasswordResetUseCase(
		repo,
		newTestPasswordResetRepo(),
		&testEmailService{},
	)

	err := uc.Execute(auth.RequestPasswordResetInput{Email: "nonexistent@test.com"})
	if err != nil {
		t.Fatalf("expected nil error for non-existent email, got: %v", err)
	}
}

func TestRequestPasswordReset_RepoCreateError(t *testing.T) {
	repo := newTestUserRepo()
	repo.users["user@test.com"] = &user.User{ID: "u1", Email: "user@test.com"}

	resetRepo := newTestPasswordResetRepo()
	resetRepo.createErr = errors.New("db error")

	uc := NewRequestPasswordResetUseCase(
		repo,
		resetRepo,
		&testEmailService{},
	)

	err := uc.Execute(auth.RequestPasswordResetInput{Email: "user@test.com"})
	if err == nil {
		t.Fatal("expected error for repo create failure")
	}
}

// seedResetUser wires a user into both lookup maps so FindByEmail and FindByID
// resolve it, mirroring how the real repo behaves.
func seedResetUser(repo *testUserRepo, id, email, password string) {
	u := &user.User{ID: id, Email: email, Password: password}
	repo.users[email] = u
	repo.byID[id] = u
}

// seedResetToken stores an account-bound token whose hash matches code.
func seedResetToken(repo *testPasswordResetRepo, userID, email, code string, expiresAt time.Time) *auth.PasswordResetToken {
	return repo.seed(&auth.PasswordResetToken{
		TokenHash: hashSecretCode(code),
		UserID:    userID,
		Email:     email,
		ExpiresAt: expiresAt,
	})
}

func TestResetPassword_Success(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	tok := seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "NewStrong1Pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if userRepo.byID["u1"].Password == "old-hash" {
		t.Error("expected password to be updated")
	}
	if !resetRepo.tokens[tok.ID].Used {
		t.Error("expected token to be marked as used")
	}
}

func TestResetPassword_WrongCode_IncrementsAndStaysInvalid(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	tok := seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "000000",
		NewPassword: "NewStrong1Pass",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got: %v", err)
	}
	if resetRepo.tokens[tok.ID].Attempts != 1 {
		t.Errorf("expected attempts=1 after one wrong guess, got %d", resetRepo.tokens[tok.ID].Attempts)
	}
	if resetRepo.tokens[tok.ID].Used {
		t.Error("token should not be burned after a single wrong guess")
	}
	if userRepo.byID["u1"].Password != "old-hash" {
		t.Error("password must not change on a wrong code")
	}
}

// The core P0.1 fix: a low-entropy code can't be brute-forced because the token
// is burned after maxResetAttempts wrong guesses, even if a later guess is right.
func TestResetPassword_BruteForce_LocksAfterMaxAttempts(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	tok := seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	for i := 0; i < maxResetAttempts; i++ {
		err := uc.Execute(auth.ResetPasswordInput{
			Email:       "user@test.com",
			Token:       "999999",
			NewPassword: "NewStrong1Pass",
		})
		if !errors.Is(err, auth.ErrInvalidResetToken) {
			t.Fatalf("guess %d: expected ErrInvalidResetToken, got: %v", i, err)
		}
	}

	if !resetRepo.tokens[tok.ID].Used {
		t.Fatal("token should be burned after reaching the attempt cap")
	}

	// Even the correct code must now fail: the token is spent.
	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "NewStrong1Pass",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected the burned token to reject the correct code, got: %v", err)
	}
	if userRepo.byID["u1"].Password != "old-hash" {
		t.Error("password must never change once the token is locked")
	}
}

// A code is only valid against the account it was issued for; presenting it for a
// different user must fail (no cross-account brute force via a single code).
func TestResetPassword_CodeBoundToAccount(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "victim", "victim@test.com", "victim-hash")
	seedResetUser(userRepo, "attacker", "attacker@test.com", "attacker-hash")

	resetRepo := newTestPasswordResetRepo()
	seedResetToken(resetRepo, "victim", "victim@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	// Attacker knows the victim's code but submits it under their own email: no
	// active token for that account, so it's rejected.
	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "attacker@test.com",
		Token:       "123456",
		NewPassword: "NewStrong1Pass",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken for cross-account code, got: %v", err)
	}
}

func TestResetPassword_MissingEmail(t *testing.T) {
	uc := NewResetPasswordUseCase(newTestUserRepo(), newTestPasswordResetRepo(), &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Token:       "123456",
		NewPassword: "NewStrong1Pass",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken when email is missing, got: %v", err)
	}
}

func TestResetPassword_NoActiveToken(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	uc := NewResetPasswordUseCase(
		userRepo,
		newTestPasswordResetRepo(),
		&testPasswordService{},
		newTestSessionRepo(),
		newTestSharedState(),
	)

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "NewStrong1",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got: %v", err)
	}
}

func TestResetPassword_UsedTokenTreatedInvalid(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	tok := seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))
	tok.Used = true

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "NewStrong1",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken for a used token, got: %v", err)
	}
}

func TestResetPassword_ExpiredToken(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(-1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "NewStrong1",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken, got: %v", err)
	}
}

func TestResetPassword_WeakPassword(t *testing.T) {
	cases := map[string]string{
		"too short":    "Ab1",
		"no uppercase": "weakpassword1",
		"no lowercase": "WEAKPASSWORD1",
		"no digit":     "WeakPasswordNoDigit",
	}
	for name, pw := range cases {
		t.Run(name, func(t *testing.T) {
			userRepo := newTestUserRepo()
			seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

			resetRepo := newTestPasswordResetRepo()
			seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

			uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

			err := uc.Execute(auth.ResetPasswordInput{
				Email:       "user@test.com",
				Token:       "123456",
				NewPassword: pw,
			})
			if !errors.Is(err, auth.ErrWeakPassword) {
				t.Fatalf("expected ErrWeakPassword, got: %v", err)
			}
		})
	}
}

// A correct code with a weak password must not consume the token: the user can
// retry with the same code once they pick a stronger password.
func TestResetPassword_WeakPasswordDoesNotBurnToken(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	tok := seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	_ = uc.Execute(auth.ResetPasswordInput{Email: "user@test.com", Token: "123456", NewPassword: "weak"})
	if resetRepo.tokens[tok.ID].Used {
		t.Fatal("a weak password after the correct code must not burn the token")
	}
	if resetRepo.tokens[tok.ID].Attempts != 0 {
		t.Errorf("a correct code must not count as a wrong attempt, got %d", resetRepo.tokens[tok.ID].Attempts)
	}

	// Retrying with the same code and a strong password now succeeds.
	if err := uc.Execute(auth.ResetPasswordInput{Email: "user@test.com", Token: "123456", NewPassword: "StrongPass1"}); err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
}

func TestResetPassword_HashError(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")

	resetRepo := newTestPasswordResetRepo()
	seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{hashErr: errors.New("hash fail")}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "StrongPass1",
	})
	if err == nil {
		t.Fatal("expected error for hash failure")
	}
}

func TestResetPassword_UserNotFound(t *testing.T) {
	userRepo := newTestUserRepo()

	resetRepo := newTestPasswordResetRepo()
	seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "StrongPass1",
	})
	if !errors.Is(err, auth.ErrInvalidResetToken) {
		t.Fatalf("expected ErrInvalidResetToken when user not found, got: %v", err)
	}
}

func TestResetPassword_UpdateError(t *testing.T) {
	userRepo := newTestUserRepo()
	seedResetUser(userRepo, "u1", "user@test.com", "old-hash")
	userRepo.updateErr = errors.New("db error")

	resetRepo := newTestPasswordResetRepo()
	seedResetToken(resetRepo, "u1", "user@test.com", "123456", time.Now().UTC().Add(1*time.Hour))

	uc := NewResetPasswordUseCase(userRepo, resetRepo, &testPasswordService{}, newTestSessionRepo(), newTestSharedState())

	err := uc.Execute(auth.ResetPasswordInput{
		Email:       "user@test.com",
		Token:       "123456",
		NewPassword: "StrongPass1",
	})
	if err == nil {
		t.Fatal("expected error for update failure")
	}
}

func TestIsStrongPassword(t *testing.T) {
	tests := []struct {
		password string
		expected bool
	}{
		{"Abcdefg1", true},
		{"StrongPass123", true},
		{"MyP@ssw0rd", true},
		{"short", false},
		{"abcdefgh1", false},
		{"ABCDEFGH1", false},
		{"Abcdefgh", false},
		{"Ab1", false},
		{"", false},
		{"12345678", false},
		{"abcdefgh", false},
		{"ABCDEFGH", false},
		{"aB1cD2eF", true},
	}

	for _, tt := range tests {
		result := isStrongPassword(tt.password)
		if result != tt.expected {
			t.Errorf("isStrongPassword(%q) = %v, want %v", tt.password, result, tt.expected)
		}
	}
}

func TestSendEmailVerification_Success(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countResp = 0

	uc := NewSendEmailVerificationUseCase(repo, &testEmailService{})

	err := uc.Execute("user@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.tokens) == 0 {
		t.Error("expected a verification token to be created")
	}
}

func TestSendEmailVerification_RateLimitExceeded(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countResp = 3

	uc := NewSendEmailVerificationUseCase(repo, &testEmailService{})

	err := uc.Execute("user@test.com")
	if !errors.Is(err, auth.ErrEmailVerificationRateExceeded) {
		t.Fatalf("expected ErrEmailVerificationRateExceeded, got: %v", err)
	}
}

func TestSendEmailVerification_CountError(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countErr = errors.New("redis error")

	uc := NewSendEmailVerificationUseCase(repo, &testEmailService{})

	err := uc.Execute("user@test.com")
	if err == nil {
		t.Fatal("expected error for count failure")
	}
}

func TestSendEmailVerification_CreateError(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countResp = 0
	repo.createErr = errors.New("create error")

	uc := NewSendEmailVerificationUseCase(repo, &testEmailService{})

	err := uc.Execute("user@test.com")
	if err == nil {
		t.Fatal("expected error for create failure")
	}
}

func TestSendEmailVerification_EmailServiceFallback(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countResp = 0

	emailSvc := &testEmailService{}

	callCount := 0
	emailSvc2 := &failFirstTemplateEmailService{callCount: &callCount}

	uc := NewSendEmailVerificationUseCase(repo, emailSvc2)

	err := uc.Execute("user@test.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = emailSvc
}

type failFirstTemplateEmailService struct {
	callCount *int
}

func (s *failFirstTemplateEmailService) SendEmail(to, subject, body string) error {
	*s.callCount++
	return nil
}
func (s *failFirstTemplateEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	return errors.New("template not found")
}

func TestSendEmailVerification_ReturnsBeforeEmailDelivery(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countResp = 0

	released := make(chan struct{})
	blocking := &blockingEmailService{release: released}

	uc := NewSendEmailVerificationUseCase(repo, blocking)

	start := time.Now()
	err := uc.Execute("user@test.com")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if elapsed > 100*time.Millisecond {
		t.Errorf("expected Execute to return quickly, took %v", elapsed)
	}

	if len(repo.tokens) == 0 {
		t.Error("expected token to be persisted synchronously")
	}

	close(released)

	time.Sleep(20 * time.Millisecond)
}

func TestSendEmailVerification_EmailFailureDoesNotFail(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.countResp = 0

	uc := NewSendEmailVerificationUseCaseSync(repo, &alwaysFailingEmailService{})

	err := uc.Execute("user@test.com")
	if err != nil {
		t.Fatalf("expected nil even when email delivery fails, got %v", err)
	}
	if len(repo.tokens) == 0 {
		t.Error("expected token to be persisted")
	}
}

type blockingEmailService struct {
	release <-chan struct{}
}

func (s *blockingEmailService) SendEmail(to, subject, body string) error {
	<-s.release
	return nil
}
func (s *blockingEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	<-s.release
	return nil
}

type alwaysFailingEmailService struct{}

func (s *alwaysFailingEmailService) SendEmail(to, subject, body string) error {
	return errors.New("smtp down")
}
func (s *alwaysFailingEmailService) SendTemplate(to, subject, templateName string, data map[string]interface{}) error {
	return errors.New("smtp down")
}

func TestVerifyEmailToken_Success(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.tokens["123456"] = &auth.EmailVerificationToken{
		Token:     "123456",
		Email:     "user@test.com",
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Used:      false,
	}

	uc := NewVerifyEmailTokenUseCase(repo)

	err := uc.Execute("123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyEmailToken_EmptyToken(t *testing.T) {
	repo := newTestEmailVerifRepo()
	uc := NewVerifyEmailTokenUseCase(repo)

	err := uc.Execute("")
	if !errors.Is(err, auth.ErrMissingVerificationToken) {
		t.Fatalf("expected ErrMissingVerificationToken, got: %v", err)
	}
}

func TestVerifyEmailToken_NotFound(t *testing.T) {
	repo := newTestEmailVerifRepo()
	uc := NewVerifyEmailTokenUseCase(repo)

	err := uc.Execute("nonexistent")
	if !errors.Is(err, auth.ErrInvalidVerificationToken) {
		t.Fatalf("expected ErrInvalidVerificationToken, got: %v", err)
	}
}

func TestVerifyEmailToken_AlreadyUsed(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.tokens["123456"] = &auth.EmailVerificationToken{
		Token:     "123456",
		Email:     "user@test.com",
		ExpiresAt: time.Now().Add(15 * time.Minute),
		Used:      true,
	}

	uc := NewVerifyEmailTokenUseCase(repo)

	err := uc.Execute("123456")
	if !errors.Is(err, auth.ErrVerificationTokenUsed) {
		t.Fatalf("expected ErrVerificationTokenUsed, got: %v", err)
	}
}

func TestVerifyEmailToken_Expired(t *testing.T) {
	repo := newTestEmailVerifRepo()
	repo.tokens["123456"] = &auth.EmailVerificationToken{
		Token:     "123456",
		Email:     "user@test.com",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
		Used:      false,
	}

	uc := NewVerifyEmailTokenUseCase(repo)

	err := uc.Execute("123456")
	if !errors.Is(err, auth.ErrInvalidVerificationToken) {
		t.Fatalf("expected ErrInvalidVerificationToken, got: %v", err)
	}
}

func TestAdminRegister_IndividualSuccess(t *testing.T) {
	userRepo := newTestUserRepo()

	uc := NewAdminRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Name:         "Admin User",
		Email:        "admin@test.com",
		Password:     "AdminPass1",
		CustomerType: "individual",
		CPF:          "12345678901",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken != "" {
		t.Error("admin register should not return access token")
	}
	if pair.Email != "admin@test.com" {
		t.Errorf("expected email admin@test.com, got %s", pair.Email)
	}
}

func TestAdminRegister_CompanySuccess(t *testing.T) {
	userRepo := newTestUserRepo()

	uc := NewAdminRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Name:         "Company",
		Email:        "company@test.com",
		Password:     "CompanyPass1",
		CustomerType: "company",
		CNPJ:         "12345678000190",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.CustomerType != "company" {
		t.Errorf("expected customerType company, got %s", pair.CustomerType)
	}
}

func TestAdminRegister_DuplicateEmail(t *testing.T) {
	userRepo := newTestUserRepo()
	userRepo.users["existing@test.com"] = &user.User{ID: "u1", Email: "existing@test.com"}

	uc := NewAdminRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "Dup",
		Email:        "existing@test.com",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "12345678901",
	})
	if !errors.Is(err, auth.ErrEmailAlreadyExists) {
		t.Fatalf("expected ErrEmailAlreadyExists, got: %v", err)
	}
}

func TestAdminRegister_InvalidCustomerType(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "u@t.com",
		Password:     "Pass123",
		CustomerType: "unknown",
	})
	if !errors.Is(err, auth.ErrInvalidCustomerType) {
		t.Fatalf("expected ErrInvalidCustomerType, got: %v", err)
	}
}

func TestAdminRegister_EmptyCustomerType(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:     "User",
		Email:    "u@t.com",
		Password: "Pass123",
	})
	if !errors.Is(err, auth.ErrInvalidCustomerType) {
		t.Fatalf("expected ErrInvalidCustomerType, got: %v", err)
	}
}

func TestAdminRegister_MissingCPF(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "u@t.com",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "",
	})
	if !errors.Is(err, auth.ErrMissingCustomerDocument) {
		t.Fatalf("expected ErrMissingCustomerDocument, got: %v", err)
	}
}

func TestAdminRegister_InvalidCPF(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "u@t.com",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "123",
	})
	if !errors.Is(err, auth.ErrInvalidCustomerDocument) {
		t.Fatalf("expected ErrInvalidCustomerDocument, got: %v", err)
	}
}

func TestAdminRegister_MissingCNPJ(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "Company",
		Email:        "c@t.com",
		Password:     "Pass123",
		CustomerType: "company",
		CNPJ:         "",
	})
	if !errors.Is(err, auth.ErrMissingCustomerDocument) {
		t.Fatalf("expected ErrMissingCustomerDocument, got: %v", err)
	}
}

func TestAdminRegister_InvalidCNPJ(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "Company",
		Email:        "c@t.com",
		Password:     "Pass123",
		CustomerType: "company",
		CNPJ:         "123",
	})
	if !errors.Is(err, auth.ErrInvalidCustomerDocument) {
		t.Fatalf("expected ErrInvalidCustomerDocument, got: %v", err)
	}
}

func TestAdminRegister_HashError(t *testing.T) {
	uc := NewAdminRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{hashErr: errors.New("hash fail")},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "u@t.com",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "12345678901",
	})
	if err == nil {
		t.Fatal("expected error for hash failure")
	}
}

func TestAdminRegister_CreateError(t *testing.T) {
	userRepo := newTestUserRepo()
	userRepo.createErr = errors.New("db error")

	uc := NewAdminRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "u@t.com",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "12345678901",
	})
	if err == nil {
		t.Fatal("expected error for create failure")
	}
}

func TestAdminRegister_NilMetricRecorder(t *testing.T) {
	userRepo := newTestUserRepo()

	uc := NewAdminRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		nil,
		&stubEnsureDefaultWs{},
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "u@t.com",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "12345678901",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestAdminRegister_EmailNormalization(t *testing.T) {
	userRepo := newTestUserRepo()

	uc := NewAdminRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testEmailService{},
		&testDocValidator{},
		&testCustomerRepo{},
		&testRecordMetric{},
		&stubEnsureDefaultWs{},
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:         "User",
		Email:        "  USER@TEST.COM  ",
		Password:     "Pass123",
		CustomerType: "individual",
		CPF:          "12345678901",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, exists := userRepo.users["user@test.com"]; !exists {
		t.Error("expected email to be normalized to lowercase")
	}
}

func TestRegister_VerificationTokenInvalid(t *testing.T) {
	userRepo := newTestUserRepo()
	uc := NewRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{err: auth.ErrInvalidVerificationToken},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@t.com",
		Password:          "Pass123",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "bad-token",
	})
	if !errors.Is(err, auth.ErrInvalidVerificationToken) {
		t.Fatalf("expected ErrInvalidVerificationToken, got: %v", err)
	}
}

func TestRegister_IndividualWithValidCPF(t *testing.T) {
	userRepo := newTestUserRepo()
	uc := NewRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Name:              "Individual User",
		Email:             "ind@test.com",
		Password:          "Strong1Pass",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair == nil {
		t.Fatal("expected non-nil token pair")
	}

	created := userRepo.users["ind@test.com"]
	if created == nil {
		t.Fatal("expected user to be created")
	}
	if !created.EmailVerified {
		t.Error("expected EmailVerified to be true")
	}
	if created.Role != user.RoleUser {
		t.Errorf("expected role user, got %s", created.Role)
	}
}

func TestRegister_CompanyWithValidCNPJ(t *testing.T) {
	userRepo := newTestUserRepo()
	uc := NewRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	pair, err := uc.Execute(auth.CredentialsInput{
		Name:              "Company",
		Email:             "company@test.com",
		Password:          "Company1Pass",
		CustomerType:      "company",
		CNPJ:              "12345678000190",
		VerificationToken: "valid-token",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair == nil {
		t.Fatal("expected non-nil token pair")
	}
}

func TestRegister_EmptyCustomerType(t *testing.T) {
	uc := NewRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@t.com",
		Password:          "StrongPass1",
		VerificationToken: "valid-token",
	})
	if !errors.Is(err, auth.ErrInvalidCustomerType) {
		t.Fatalf("expected ErrInvalidCustomerType, got: %v", err)
	}
}

func TestRegister_MissingCPF(t *testing.T) {
	uc := NewRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@t.com",
		Password:          "StrongPass1",
		CustomerType:      "individual",
		VerificationToken: "valid-token",
	})
	if !errors.Is(err, auth.ErrMissingCustomerDocument) {
		t.Fatalf("expected ErrMissingCustomerDocument, got: %v", err)
	}
}

func TestRegister_HashError(t *testing.T) {
	uc := NewRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{hashErr: errors.New("hash fail")},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@t.com",
		Password:          "Pass123",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})
	if err == nil {
		t.Fatal("expected error for hash failure")
	}
}

func TestRegister_CreateError(t *testing.T) {
	userRepo := newTestUserRepo()
	userRepo.createErr = errors.New("db error")

	uc := NewRegisterUseCase(
		userRepo,
		&testPasswordService{},
		&testTokenIssuer{},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@t.com",
		Password:          "Pass123",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})
	if err == nil {
		t.Fatal("expected error for create failure")
	}
}

func TestRegister_TokenIssueError(t *testing.T) {
	uc := NewRegisterUseCase(
		newTestUserRepo(),
		&testPasswordService{},
		&testTokenIssuer{issueErr: errors.New("issue fail")},
		newTestSessionRepo(),
		&testEmailService{},
		&testDocValidator{},
		&testVerifyEmailToken{},
		newTestEmailVerifRepo(),
		&testCustomerRepo{},
		&testRecordMetric{},
		nil,
	)

	_, err := uc.Execute(auth.CredentialsInput{
		Name:              "User",
		Email:             "u@t.com",
		Password:          "Pass123",
		CustomerType:      "individual",
		CPF:               "12345678901",
		VerificationToken: "valid-token",
	})
	if err == nil {
		t.Fatal("expected error for token issue failure")
	}
}
