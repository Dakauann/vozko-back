package affiliate_usecase

import (
	"context"
	"errors"
	"sync"

	"vozko/domain/affiliate"
	config_domain "vozko/domain/config"
	"vozko/domain/shared"
	user_domain "vozko/domain/user"
)

type mockRepo struct {
	mu             sync.Mutex
	affiliates     map[string]*affiliate.Affiliate
	referrals      map[string]*affiliate.Referral
	earnings       map[string]*affiliate.Earning
	failCreate     error
	failGetByID    error
	failGetByUser  error
	failGetByCode  error
	failUpdate     error
	failList       error
	failCreateRef  error
	failGetRef     error
	failListRef    error
	failCountRef   error
	failCreateEarn error
	failGetEarn    error
	failListEarn   error
	failSumAll     error
	failSumSince   error
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		affiliates: map[string]*affiliate.Affiliate{},
		referrals:  map[string]*affiliate.Referral{},
		earnings:   map[string]*affiliate.Earning{},
	}
}

func (m *mockRepo) Create(ctx context.Context, a *affiliate.Affiliate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCreate != nil {
		return m.failCreate
	}
	if _, exists := m.affiliates[a.ID]; exists {
		return errors.New("duplicate id")
	}
	for _, existing := range m.affiliates {
		if existing.UserID == a.UserID {
			return errors.New("duplicate user_id")
		}
		if existing.Code == a.Code {
			return errors.New("duplicate code")
		}
	}
	cpy := *a
	m.affiliates[a.ID] = &cpy
	return nil
}

func (m *mockRepo) GetByID(ctx context.Context, id string) (*affiliate.Affiliate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGetByID != nil {
		return nil, m.failGetByID
	}
	a, ok := m.affiliates[id]
	if !ok {
		return nil, affiliate.ErrAffiliateNotFound
	}
	cpy := *a
	return &cpy, nil
}

func (m *mockRepo) GetByUserID(ctx context.Context, userID string) (*affiliate.Affiliate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGetByUser != nil {
		return nil, m.failGetByUser
	}
	for _, a := range m.affiliates {
		if a.UserID == userID {
			cpy := *a
			return &cpy, nil
		}
	}
	return nil, affiliate.ErrAffiliateNotFound
}

func (m *mockRepo) GetByCode(ctx context.Context, code string) (*affiliate.Affiliate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGetByCode != nil {
		return nil, m.failGetByCode
	}
	for _, a := range m.affiliates {
		if a.Code == code {
			cpy := *a
			return &cpy, nil
		}
	}
	return nil, affiliate.ErrAffiliateNotFound
}

func (m *mockRepo) Update(ctx context.Context, a *affiliate.Affiliate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpdate != nil {
		return m.failUpdate
	}
	if _, ok := m.affiliates[a.ID]; !ok {
		return affiliate.ErrAffiliateNotFound
	}
	cpy := *a
	m.affiliates[a.ID] = &cpy
	return nil
}

func (m *mockRepo) List(ctx context.Context, page, pageSize int) ([]affiliate.Affiliate, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failList != nil {
		return nil, 0, m.failList
	}
	all := make([]affiliate.Affiliate, 0, len(m.affiliates))
	for _, a := range m.affiliates {
		all = append(all, *a)
	}
	total := int64(len(all))
	if pageSize <= 0 {
		return all, total, nil
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	if start >= len(all) {
		return []affiliate.Affiliate{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

func (m *mockRepo) CreateReferral(ctx context.Context, r *affiliate.Referral) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCreateRef != nil {
		return m.failCreateRef
	}
	if _, exists := m.referrals[r.WorkspaceID]; exists {
		return errors.New("duplicate workspace referral")
	}
	cpy := *r
	m.referrals[r.WorkspaceID] = &cpy
	return nil
}

func (m *mockRepo) GetReferralByWorkspaceID(ctx context.Context, workspaceID string) (*affiliate.Referral, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGetRef != nil {
		return nil, m.failGetRef
	}
	r, ok := m.referrals[workspaceID]
	if !ok {
		return nil, nil
	}
	cpy := *r
	return &cpy, nil
}

func (m *mockRepo) ListReferralsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]affiliate.Referral, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failListRef != nil {
		return nil, 0, m.failListRef
	}
	out := []affiliate.Referral{}
	for _, r := range m.referrals {
		if r.AffiliateID == affiliateID {
			out = append(out, *r)
		}
	}
	return out, int64(len(out)), nil
}

func (m *mockRepo) CountReferralsByAffiliateID(ctx context.Context, affiliateID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCountRef != nil {
		return 0, m.failCountRef
	}
	var n int64
	for _, r := range m.referrals {
		if r.AffiliateID == affiliateID {
			n++
		}
	}
	return n, nil
}

func (m *mockRepo) CreateEarning(ctx context.Context, e *affiliate.Earning) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failCreateEarn != nil {
		return m.failCreateEarn
	}
	if _, exists := m.earnings[e.InvoiceID]; exists {
		return errors.New("duplicate earning")
	}
	cpy := *e
	m.earnings[e.InvoiceID] = &cpy
	return nil
}

func (m *mockRepo) GetEarningByInvoiceID(ctx context.Context, invoiceID string) (*affiliate.Earning, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failGetEarn != nil {
		return nil, m.failGetEarn
	}
	e, ok := m.earnings[invoiceID]
	if !ok {
		return nil, nil
	}
	cpy := *e
	return &cpy, nil
}

func (m *mockRepo) ListEarningsByAffiliateID(ctx context.Context, affiliateID string, page, pageSize int) ([]affiliate.Earning, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failListEarn != nil {
		return nil, 0, m.failListEarn
	}
	out := []affiliate.Earning{}
	for _, e := range m.earnings {
		if e.AffiliateID == affiliateID {
			out = append(out, *e)
		}
	}
	return out, int64(len(out)), nil
}

func (m *mockRepo) SumEarningsByAffiliateID(ctx context.Context, affiliateID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSumAll != nil {
		return 0, m.failSumAll
	}
	var total int64
	for _, e := range m.earnings {
		if e.AffiliateID == affiliateID {
			total += e.AmountMicros
		}
	}
	return total, nil
}

func (m *mockRepo) SumEarningsSince(ctx context.Context, affiliateID string, sinceUnix int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSumSince != nil {
		return 0, m.failSumSince
	}
	var total int64
	for _, e := range m.earnings {
		if e.AffiliateID == affiliateID && e.CreatedAt.Unix() >= sinceUnix {
			total += e.AmountMicros
		}
	}
	return total, nil
}

type mockUserRepo struct {
	users map[string]*user_domain.User
}

func newMockUserRepo(ids ...string) *mockUserRepo {
	m := &mockUserRepo{users: map[string]*user_domain.User{}}
	for _, id := range ids {
		m.users[id] = &user_domain.User{ID: id, Username: "tester", CPF: "11144477735"}
	}
	return m
}

func (m *mockUserRepo) WithTx(tx interface{}) user_domain.UserRepository { return m }
func (m *mockUserRepo) Create(u *user_domain.User) error                 { panic("unused") }
func (m *mockUserRepo) Update(id string, u *user_domain.User) error      { panic("unused") }
func (m *mockUserRepo) Delete(id string) error                           { panic("unused") }
func (m *mockUserRepo) FindByID(id string) (*user_domain.User, error) {
	if u, ok := m.users[id]; ok {
		cpy := *u
		return &cpy, nil
	}
	return nil, errors.New("user not found")
}
func (m *mockUserRepo) FindByIDs(ids []string) ([]*user_domain.User, error) { panic("unused") }
func (m *mockUserRepo) FindByEmail(email string) (*user_domain.User, error) { panic("unused") }
func (m *mockUserRepo) FindByDocument(doc string) (*user_domain.User, error) {
	panic("unused")
}
func (m *mockUserRepo) List(input user_domain.ListUsersInput) (*shared.PaginatedResult[*user_domain.User], error) {
	panic("unused")
}
func (m *mockUserRepo) CountByRole(role user_domain.Role) (int64, error) { panic("unused") }
func (m *mockUserRepo) GetUserRole(id string) (string, error)            { panic("unused") }
func (m *mockUserRepo) GetTokenVersion(id string) (int, error)           { panic("unused") }
func (m *mockUserRepo) IncrementTokenVersion(id string) (int, error)     { panic("unused") }

type mockSystemConfigRepo struct {
	cfg *config_domain.SystemConfig
	err error
}

func (m *mockSystemConfigRepo) Get(ctx context.Context) (*config_domain.SystemConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.cfg, nil
}
func (m *mockSystemConfigRepo) Upsert(ctx context.Context, cfg *config_domain.SystemConfig) error {
	m.cfg = cfg
	return nil
}

type mockWalletValidator struct {
	calls []affiliate.WalletValidationInput
	err   error
}

func (m *mockWalletValidator) ValidateWallet(_ context.Context, in affiliate.WalletValidationInput) error {
	m.calls = append(m.calls, in)
	return m.err
}

type mockExchangeRateProvider struct {
	rateMicros int64
	err        error
	calls      int
}

func (m *mockExchangeRateProvider) CurrentRateMicros(_ context.Context) (int64, error) {
	m.calls++
	return m.rateMicros, m.err
}
