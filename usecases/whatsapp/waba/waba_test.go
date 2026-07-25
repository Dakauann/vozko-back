package waba_usecase

import (
	"testing"

	"vozko/domain/shared"
	waba_domain "vozko/domain/whatsapp/waba"
)

type mockWABARepository struct {
	accounts map[string]*waba_domain.WhatsAppBusinessAccount
}

func newMockWABARepo() *mockWABARepository {
	return &mockWABARepository{
		accounts: make(map[string]*waba_domain.WhatsAppBusinessAccount),
	}
}

func (m *mockWABARepository) Create(a *waba_domain.WhatsAppBusinessAccount) error {
	if a.ID == "" {
		a.ID = "gen-id"
	}
	for _, existing := range m.accounts {
		if existing.MetaWABAId == a.MetaWABAId {
			return waba_domain.ErrWABAAlreadyExists
		}
	}
	m.accounts[a.ID] = a
	return nil
}

func (m *mockWABARepository) Update(id string, a *waba_domain.WhatsAppBusinessAccount) error {
	m.accounts[id] = a
	return nil
}

func (m *mockWABARepository) Delete(id string) error {
	delete(m.accounts, id)
	return nil
}

func (m *mockWABARepository) FindByID(id string) (*waba_domain.WhatsAppBusinessAccount, error) {
	a, ok := m.accounts[id]
	if !ok {
		return nil, waba_domain.ErrWABANotFound
	}
	return a, nil
}

func (m *mockWABARepository) FindByMetaWABAId(metaWABAId string) (*waba_domain.WhatsAppBusinessAccount, error) {
	for _, a := range m.accounts {
		if a.MetaWABAId == metaWABAId {
			return a, nil
		}
	}
	return nil, waba_domain.ErrWABANotFound
}

func (m *mockWABARepository) List(_ waba_domain.ListInput) (*shared.PaginatedResult[*waba_domain.WhatsAppBusinessAccount], error) {
	var items []*waba_domain.WhatsAppBusinessAccount
	for _, a := range m.accounts {
		items = append(items, a)
	}
	return &shared.PaginatedResult[*waba_domain.WhatsAppBusinessAccount]{
		Items:      items,
		TotalItems: int64(len(items)),
		Page:       1,
		PageSize:   20,
		TotalPages: 1,
	}, nil
}

func (m *mockWABARepository) ClearAccessToken(id string) error {
	if a, ok := m.accounts[id]; ok {
		a.AccessToken = ""
		return nil
	}
	return waba_domain.ErrWABANotFound
}

func TestListUseCase_ReturnsAccounts(t *testing.T) {
	repo := newMockWABARepo()
	repo.accounts["w1"] = &waba_domain.WhatsAppBusinessAccount{
		ID:         "w1",
		MetaWABAId: "meta_w1",
		Name:       "WABA One",
	}
	repo.accounts["w2"] = &waba_domain.WhatsAppBusinessAccount{
		ID:         "w2",
		MetaWABAId: "meta_w2",
		Name:       "WABA Two",
	}

	uc := NewListUseCase(repo)
	result, err := uc.Execute(waba_domain.ListInput{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", result.TotalItems)
	}
}

func TestListUseCase_EmptyResult(t *testing.T) {
	repo := newMockWABARepo()
	uc := NewListUseCase(repo)

	result, err := uc.Execute(waba_domain.ListInput{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.TotalItems != 0 {
		t.Errorf("TotalItems = %d, want 0", result.TotalItems)
	}
}

func TestGetUseCase_Found(t *testing.T) {
	repo := newMockWABARepo()
	repo.accounts["w1"] = &waba_domain.WhatsAppBusinessAccount{
		ID:         "w1",
		MetaWABAId: "meta_w1",
		Name:       "WABA One",
	}

	uc := NewGetUseCase(repo)
	account, err := uc.Execute("w1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if account.Name != "WABA One" {
		t.Errorf("Name = %s, want WABA One", account.Name)
	}
}

func TestGetUseCase_NotFound(t *testing.T) {
	repo := newMockWABARepo()
	uc := NewGetUseCase(repo)

	_, err := uc.Execute("nonexistent")
	if err != waba_domain.ErrWABANotFound {
		t.Errorf("error = %v, want ErrWABANotFound", err)
	}
}

func TestNewWhatsAppBusinessAccount_Valid(t *testing.T) {
	a, err := waba_domain.NewWhatsAppBusinessAccount("12345", "My WABA")
	if err != nil {
		t.Fatalf("NewWhatsAppBusinessAccount: %v", err)
	}
	if a.MetaWABAId != "12345" {
		t.Errorf("MetaWABAId = %s, want 12345", a.MetaWABAId)
	}
	if a.Name != "My WABA" {
		t.Errorf("Name = %s, want My WABA", a.Name)
	}
}

func TestNewWhatsAppBusinessAccount_EmptyMetaID(t *testing.T) {
	_, err := waba_domain.NewWhatsAppBusinessAccount("", "My WABA")
	if err != waba_domain.ErrMetaWABAIDRequired {
		t.Errorf("error = %v, want ErrMetaWABAIDRequired", err)
	}
}

func TestNewWhatsAppBusinessAccount_WhitespaceMetaID(t *testing.T) {
	_, err := waba_domain.NewWhatsAppBusinessAccount("   ", "My WABA")
	if err != waba_domain.ErrMetaWABAIDRequired {
		t.Errorf("error = %v, want ErrMetaWABAIDRequired", err)
	}
}

func TestNewWhatsAppBusinessAccount_TrimsWhitespace(t *testing.T) {
	a, err := waba_domain.NewWhatsAppBusinessAccount("  12345  ", "  My WABA  ")
	if err != nil {
		t.Fatalf("NewWhatsAppBusinessAccount: %v", err)
	}
	if a.MetaWABAId != "12345" {
		t.Errorf("MetaWABAId = %q, want 12345", a.MetaWABAId)
	}
	if a.Name != "My WABA" {
		t.Errorf("Name = %q, want My WABA", a.Name)
	}
}
