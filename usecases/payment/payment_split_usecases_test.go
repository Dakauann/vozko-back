package payment_usecase

import (
	"errors"
	"testing"

	"vozko/domain/payment"
)

type memSplitRepo struct {
	splits   map[string]*payment.PaymentSplit
	createID int
}

func newMemSplitRepo() *memSplitRepo {
	return &memSplitRepo{splits: make(map[string]*payment.PaymentSplit)}
}

func (r *memSplitRepo) Create(s *payment.PaymentSplit) error {
	if s.ID == "" {
		r.createID++
	}
	r.splits[s.ID] = s
	return nil
}

func (r *memSplitRepo) Update(s *payment.PaymentSplit) error {
	if _, ok := r.splits[s.ID]; !ok {
		return payment.ErrPaymentSplitNotFound
	}
	r.splits[s.ID] = s
	return nil
}

func (r *memSplitRepo) Delete(id string) error {
	delete(r.splits, id)
	return nil
}

func (r *memSplitRepo) GetByID(id string) (*payment.PaymentSplit, error) {
	s, ok := r.splits[id]
	if !ok {
		return nil, payment.ErrPaymentSplitNotFound
	}
	return s, nil
}

func (r *memSplitRepo) GetByIDs(ids []string) (map[string]*payment.PaymentSplit, error) {
	result := make(map[string]*payment.PaymentSplit)
	for _, id := range ids {
		if s, ok := r.splits[id]; ok {
			result[id] = s
		}
	}
	return result, nil
}

func (r *memSplitRepo) GetByType(t payment.SplitType) ([]*payment.PaymentSplit, error) {
	var result []*payment.PaymentSplit
	for _, s := range r.splits {
		if s.Type == t {
			result = append(result, s)
		}
	}
	return result, nil
}

func (r *memSplitRepo) GetSuppliers() ([]*payment.PaymentSplit, error) {
	return r.GetByType(payment.SplitTypeSupplier)
}

func (r *memSplitRepo) List() ([]payment.PaymentSplit, error) {
	var result []payment.PaymentSplit
	for _, s := range r.splits {
		result = append(result, *s)
	}
	return result, nil
}

type errSplitRepo struct {
	memSplitRepo
	createErr  error
	updateErr  error
	getByIDErr error
}

func newErrSplitRepo() *errSplitRepo {
	return &errSplitRepo{memSplitRepo: *newMemSplitRepo()}
}

func (r *errSplitRepo) Create(s *payment.PaymentSplit) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.memSplitRepo.Create(s)
}

func (r *errSplitRepo) Update(s *payment.PaymentSplit) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.memSplitRepo.Update(s)
}

func (r *errSplitRepo) GetByID(id string) (*payment.PaymentSplit, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	return r.memSplitRepo.GetByID(id)
}

func validSplit() *payment.PaymentSplit {
	return &payment.PaymentSplit{
		ID:       "split-1",
		Name:     "Supplier A",
		Type:     payment.SplitTypeSupplier,
		Provider: payment.SplitProviderAsaas,
		WalletID: "wallet-1",
	}
}

func TestCreatePaymentSplit_Success(t *testing.T) {
	repo := newMemSplitRepo()
	uc := NewCreatePaymentSplitUseCase(repo)
	s := validSplit()
	result, err := uc.Execute(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "split-1" {
		t.Fatalf("expected split-1, got %s", result.ID)
	}
}

func TestCreatePaymentSplit_NilSplit(t *testing.T) {
	uc := NewCreatePaymentSplitUseCase(newMemSplitRepo())
	_, err := uc.Execute(nil)
	if !errors.Is(err, payment.ErrPaymentSplitNameRequired) {
		t.Fatalf("expected ErrPaymentSplitNameRequired, got %v", err)
	}
}

func TestCreatePaymentSplit_InvalidType(t *testing.T) {
	uc := NewCreatePaymentSplitUseCase(newMemSplitRepo())
	s := validSplit()
	s.Type = "bogus"
	_, err := uc.Execute(s)
	if !errors.Is(err, payment.ErrInvalidSplitType) {
		t.Fatalf("expected ErrInvalidSplitType, got %v", err)
	}
}

func TestCreatePaymentSplit_InvalidProvider(t *testing.T) {
	uc := NewCreatePaymentSplitUseCase(newMemSplitRepo())
	s := validSplit()
	s.Provider = "bogus"
	_, err := uc.Execute(s)
	if !errors.Is(err, payment.ErrInvalidSplitProvider) {
		t.Fatalf("expected ErrInvalidSplitProvider, got %v", err)
	}
}

func TestCreatePaymentSplit_RepoCreateError(t *testing.T) {
	repo := newErrSplitRepo()
	repo.createErr = errors.New("db error")
	uc := NewCreatePaymentSplitUseCase(repo)
	_, err := uc.Execute(validSplit())
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected db error, got %v", err)
	}
}

func TestCreatePaymentSplit_ValidationError_EmptyName(t *testing.T) {
	uc := NewCreatePaymentSplitUseCase(newMemSplitRepo())
	s := validSplit()
	s.Name = " "
	_, err := uc.Execute(s)
	if !errors.Is(err, payment.ErrPaymentSplitNameRequired) {
		t.Fatalf("expected ErrPaymentSplitNameRequired, got %v", err)
	}
}

func TestCreatePaymentSplit_ValidationError_EmptyWallet(t *testing.T) {
	uc := NewCreatePaymentSplitUseCase(newMemSplitRepo())
	s := validSplit()
	s.WalletID = ""
	_, err := uc.Execute(s)
	if !errors.Is(err, payment.ErrPaymentSplitWalletRequired) {
		t.Fatalf("expected ErrPaymentSplitWalletRequired, got %v", err)
	}
}

func TestCreatePaymentSplit_ValidationError_OwnerNeedsPercentage(t *testing.T) {
	uc := NewCreatePaymentSplitUseCase(newMemSplitRepo())
	s := validSplit()
	s.Type = payment.SplitTypeOwner
	s.Percentage = 0
	_, err := uc.Execute(s)
	if !errors.Is(err, payment.ErrInvalidSplitPercentage) {
		t.Fatalf("expected ErrInvalidSplitPercentage, got %v", err)
	}
}

func TestUpdatePaymentSplit_Success(t *testing.T) {
	repo := newMemSplitRepo()
	s := validSplit()
	repo.Create(s)
	uc := NewUpdatePaymentSplitUseCase(repo)
	updated := &payment.PaymentSplit{
		Name:     "Updated",
		Type:     payment.SplitTypeSupplier,
		Provider: payment.SplitProviderAsaas,
		WalletID: "wallet-2",
	}
	result, err := uc.Execute("split-1", updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "Updated" || result.WalletID != "wallet-2" {
		t.Fatalf("expected updated split, got %+v", result)
	}
}

func TestUpdatePaymentSplit_EmptyID(t *testing.T) {
	uc := NewUpdatePaymentSplitUseCase(newMemSplitRepo())
	_, err := uc.Execute("", &payment.PaymentSplit{})
	if !errors.Is(err, payment.ErrPaymentSplitNotFound) {
		t.Fatalf("expected ErrPaymentSplitNotFound, got %v", err)
	}
}

func TestUpdatePaymentSplit_NotFound(t *testing.T) {
	uc := NewUpdatePaymentSplitUseCase(newMemSplitRepo())
	_, err := uc.Execute("nope", &payment.PaymentSplit{})
	if !errors.Is(err, payment.ErrPaymentSplitNotFound) {
		t.Fatalf("expected ErrPaymentSplitNotFound, got %v", err)
	}
}

func TestUpdatePaymentSplit_ValidationError(t *testing.T) {
	repo := newMemSplitRepo()
	repo.Create(validSplit())
	uc := NewUpdatePaymentSplitUseCase(repo)
	_, err := uc.Execute("split-1", &payment.PaymentSplit{
		Name:     "ok",
		Type:     "bogus",
		Provider: payment.SplitProviderAsaas,
		WalletID: "w",
	})
	if !errors.Is(err, payment.ErrInvalidSplitType) {
		t.Fatalf("expected ErrInvalidSplitType, got %v", err)
	}
}

func TestUpdatePaymentSplit_RepoUpdateError(t *testing.T) {
	repo := newErrSplitRepo()
	repo.Create(validSplit())
	repo.updateErr = errors.New("db error")
	uc := NewUpdatePaymentSplitUseCase(repo)
	_, err := uc.Execute("split-1", &payment.PaymentSplit{
		Name:     "ok",
		Type:     payment.SplitTypeSupplier,
		Provider: payment.SplitProviderAsaas,
		WalletID: "w",
	})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected db error, got %v", err)
	}
}

func TestDeletePaymentSplit_Success(t *testing.T) {
	repo := newMemSplitRepo()
	repo.Create(validSplit())
	uc := NewDeletePaymentSplitUseCase(repo)
	if err := uc.Execute("split-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeletePaymentSplit_EmptyID(t *testing.T) {
	uc := NewDeletePaymentSplitUseCase(newMemSplitRepo())
	if err := uc.Execute(""); !errors.Is(err, payment.ErrPaymentSplitNotFound) {
		t.Fatalf("expected ErrPaymentSplitNotFound, got %v", err)
	}
}

func TestGetPaymentSplit_Success(t *testing.T) {
	repo := newMemSplitRepo()
	repo.Create(validSplit())
	uc := NewGetPaymentSplitUseCase(repo)
	s, err := uc.Execute("split-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ID != "split-1" {
		t.Fatalf("expected split-1, got %s", s.ID)
	}
}

func TestGetPaymentSplit_EmptyID(t *testing.T) {
	uc := NewGetPaymentSplitUseCase(newMemSplitRepo())
	_, err := uc.Execute("")
	if !errors.Is(err, payment.ErrPaymentSplitNotFound) {
		t.Fatalf("expected ErrPaymentSplitNotFound, got %v", err)
	}
}

func TestListPaymentSplits(t *testing.T) {
	repo := newMemSplitRepo()
	repo.Create(validSplit())
	uc := NewListPaymentSplitsUseCase(repo)
	items, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestGetPaymentSplitSuppliers(t *testing.T) {
	repo := newMemSplitRepo()
	repo.Create(validSplit())
	uc := NewGetPaymentSplitSuppliersUseCase(repo)
	items, err := uc.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 supplier, got %d", len(items))
	}
}
